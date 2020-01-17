package storageimpl

import (
	"bytes"
	"context"

	ipldfree "github.com/ipld/go-ipld-prime/impl/free"
	"github.com/ipld/go-ipld-prime/traversal/selector"
	"github.com/ipld/go-ipld-prime/traversal/selector/builder"

	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-fil-markets/piecestore"
	"github.com/filecoin-project/go-fil-markets/shared/tokenamount"
	"github.com/filecoin-project/go-fil-markets/storagemarket"
)

type providerHandlerFunc func(ctx context.Context, deal MinerDeal) (func(*MinerDeal), error)

func (p *Provider) handle(ctx context.Context, deal MinerDeal, cb providerHandlerFunc, next storagemarket.DealState) {
	go func() {
		mut, err := cb(ctx, deal)

		if err == nil && next == storagemarket.DealNoUpdate {
			return
		}

		select {
		case p.updated <- minerDealUpdate{
			newState: next,
			id:       deal.ProposalCid,
			err:      err,
			mut:      mut,
		}:
		case <-p.stop:
		}
	}()
}

// DealValidating
func (p *Provider) validating(ctx context.Context, deal MinerDeal) (func(*MinerDeal), error) {
	head, err := p.spn.MostRecentStateId(ctx)
	if err != nil {
		return nil, err
	}
	if head.Height() >= deal.Proposal.ProposalExpiration {
		return nil, xerrors.Errorf("deal proposal already expired")
	}

	// TODO: check StorageCollateral

	minPrice := tokenamount.Div(tokenamount.Mul(p.ask.Ask.Price, tokenamount.FromInt(deal.Proposal.PieceSize)), tokenamount.FromInt(1<<30))
	if deal.Proposal.StoragePricePerEpoch.LessThan(minPrice) {
		return nil, xerrors.Errorf("storage price per epoch less than asking price: %s < %s", deal.Proposal.StoragePricePerEpoch, minPrice)
	}

	if deal.Proposal.PieceSize < p.ask.Ask.MinPieceSize {
		return nil, xerrors.Errorf("piece size less than minimum required size: %d < %d", deal.Proposal.PieceSize, p.ask.Ask.MinPieceSize)
	}

	// check market funds
	clientMarketBalance, err := p.spn.GetBalance(ctx, deal.Proposal.Client)
	if err != nil {
		return nil, xerrors.Errorf("getting client market balance failed: %w", err)
	}

	// This doesn't guarantee that the client won't withdraw / lock those funds
	// but it's a decent first filter
	if clientMarketBalance.Available.LessThan(deal.Proposal.TotalStoragePrice()) {
		return nil, xerrors.New("clientMarketBalance.Available too small")
	}

	// TODO: Send intent to accept
	return nil, nil
}

// State: DealTransferring
func (p *Provider) transferring(ctx context.Context, deal MinerDeal) (func(*MinerDeal), error) {
	ssb := builder.NewSelectorSpecBuilder(ipldfree.NodeBuilder())

	// this is the selector for "get the whole DAG"
	// TODO: support storage deals with custom payload selectors
	allSelector := ssb.ExploreRecursive(selector.RecursionLimitNone(),
		ssb.ExploreAll(ssb.ExploreRecursiveEdge())).Node()

	log.Infof("fetching data for a deal %d", deal.ProposalCid)

	// initiate a pull data transfer. This will complete asynchronously and the
	// completion of the data transfer will trigger a change in deal state
	// (see onDataTransferEvent)
	_, err := p.dataTransfer.OpenPullDataChannel(ctx,
		deal.Client,
		&StorageDataTransferVoucher{Proposal: deal.ProposalCid},
		deal.Ref,
		allSelector,
	)
	if err != nil {
		return nil, xerrors.Errorf("failed to open pull data channel: %w", err)
	}

	return nil, nil
}

// State: DealVerifyData
func (p *Provider) verifydata(ctx context.Context, deal MinerDeal) (func(*MinerDeal), error) {
	// entire DAG selector
	ssb := builder.NewSelectorSpecBuilder(ipldfree.NodeBuilder())
	allSelector := ssb.ExploreRecursive(selector.RecursionLimitNone(),
		ssb.ExploreAll(ssb.ExploreRecursiveEdge())).Node()

	commp, file, err := p.pio.GeneratePieceCommitment(deal.Ref, allSelector)
	if err != nil {
		return nil, err
	}

	// Verify CommP matches
	if !bytes.Equal(commp, deal.Proposal.PieceRef) {
		return nil, xerrors.Errorf("proposal CommP doesn't match calculated CommP")
	}

	return func(deal *MinerDeal) {
		deal.PiecePath = file.Path()
	}, nil
}

// State: DealPublishing
func (p *Provider) publishing(ctx context.Context, deal MinerDeal) (func(*MinerDeal), error) {
	waddr, err := p.spn.GetMinerWorker(ctx, deal.Proposal.Provider)
	if err != nil {
		return nil, err
	}

	// TODO: check StorageCollateral (may be too large (or too small))
	if err := p.spn.EnsureFunds(ctx, waddr, deal.Proposal.StorageCollateral); err != nil {
		return nil, err
	}

	smDeal := storagemarket.MinerDeal{
		Client:      deal.Client,
		Proposal:    deal.Proposal,
		ProposalCid: deal.ProposalCid,
		State:       deal.State,
		Ref:         deal.Ref,
		SectorID:    deal.SectorID,
	}

	dealId, mcid, err := p.spn.PublishDeals(ctx, smDeal)
	if err != nil {
		return nil, err
	}

	err = p.sendSignedResponse(ctx, &Response{
		State: storagemarket.DealAccepted,

		Proposal:       deal.ProposalCid,
		PublishMessage: &mcid,
	})
	if err != nil {
		return nil, err
	}

	if err := p.disconnect(deal); err != nil {
		log.Warnf("closing client connection: %+v", err)
	}

	return func(deal *MinerDeal) {
		deal.DealID = uint64(dealId)
	}, nil
}

// STAGED
func (p *Provider) staged(ctx context.Context, deal MinerDeal) (func(*MinerDeal), error) {
	sectorID, err := p.spn.OnDealComplete(
		ctx,
		storagemarket.MinerDeal{
			Client:      deal.Client,
			Proposal:    deal.Proposal,
			ProposalCid: deal.ProposalCid,
			State:       deal.State,
			Ref:         deal.Ref,
			DealID:      deal.DealID,
		},
		string(deal.PiecePath),
	)

	if err != nil {
		return nil, err
	}

	return func(deal *MinerDeal) {
		deal.SectorID = sectorID
	}, nil
}

// SEALING

func (p *Provider) sealing(ctx context.Context, deal MinerDeal) (func(*MinerDeal), error) {
	// TODO: consider waiting for seal to happen
	cb := func(err error) {
		select {
		case p.updated <- minerDealUpdate{
			newState: storagemarket.DealComplete,
			id:       deal.ProposalCid,
			err:      err,
		}:
		case <-p.stop:
		}
	}

	err := p.spn.OnDealSectorCommitted(ctx, deal.Proposal.Provider, deal.DealID, cb)

	return nil, err
}

func (p *Provider) complete(ctx context.Context, deal MinerDeal) (func(*MinerDeal), error) {
	// TODO: observe sector lifecycle, status, expiration..
	sectorID, offset, length, err := p.spn.LocatePieceForDealWithinSector(ctx, deal.DealID)
	if err != nil {
		return nil, err
	}
	return nil, p.pieceStore.AddDealForPiece(deal.Proposal.PieceRef, piecestore.DealInfo{
		DealID:   deal.DealID,
		SectorID: sectorID,
		Offset:   offset,
		Length:   length,
	})
}
