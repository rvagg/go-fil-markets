package storageimpl

import (
	"context"
	"runtime"

	"github.com/ipld/go-ipld-prime"

	"github.com/filecoin-project/go-data-transfer"

	"github.com/filecoin-project/go-fil-markets/storagemarket"

	"github.com/filecoin-project/go-cbor-util"
	"github.com/filecoin-project/go-statestore"

	"github.com/ipfs/go-cid"
	inet "github.com/libp2p/go-libp2p-core/network"
	"github.com/libp2p/go-libp2p-core/peer"
	"golang.org/x/xerrors"
)

func (p *Provider) failDeal(ctx context.Context, id cid.Cid, cerr error) {
	if err := p.deals.Get(id).End(); err != nil {
		log.Warnf("deals.End: %s", err)
	}

	if cerr == nil {
		_, f, l, _ := runtime.Caller(1)
		cerr = xerrors.Errorf("unknown error (fail called at %s:%d)", f, l)
	}

	log.Warnf("deal %s failed: %s", id, cerr)

	err := p.sendSignedResponse(ctx, &Response{
		State:    storagemarket.DealFailed,
		Message:  cerr.Error(),
		Proposal: id,
	})

	s, ok := p.conns[id]
	if ok {
		_ = s.Reset()
		delete(p.conns, id)
	}

	if err != nil {
		log.Warnf("notifying client about deal failure: %s", err)
	}
}

func (p *Provider) readProposal(s inet.Stream) (proposal Proposal, err error) {
	if err := cborutil.ReadCborRPC(s, &proposal); err != nil {
		log.Errorw("failed to read proposal message", "error", err)
		return proposal, err
	}

	if proposal.DealProposal.ProposerSignature == nil {
		return proposal, xerrors.Errorf("incoming deal proposal has no signature")
	}

	if err := proposal.DealProposal.Verify(); err != nil {
		return proposal, xerrors.Errorf("verifying StorageDealProposal: %w", err)
	}

	if proposal.DealProposal.Provider != p.actor {
		log.Errorf("proposal with wrong ProviderAddress: %s", proposal.DealProposal.Provider)
		return proposal, err
	}

	return
}

func (p *Provider) sendSignedResponse(ctx context.Context, resp *Response) error {
	s, ok := p.conns[resp.Proposal]
	if !ok {
		return xerrors.New("couldn't send response: not connected")
	}

	msg, err := cborutil.Dump(resp)
	if err != nil {
		return xerrors.Errorf("serializing response: %w", err)
	}

	worker, err := p.spn.GetMinerWorker(ctx, p.actor)
	if err != nil {
		return err
	}

	sig, err := p.spn.SignBytes(ctx, worker, msg)
	if err != nil {
		return xerrors.Errorf("failed to sign response message: %w", err)
	}

	signedResponse := &SignedResponse{
		Response:  *resp,
		Signature: sig,
	}

	err = cborutil.WriteCborRPC(s, signedResponse)
	if err != nil {
		// Assume client disconnected
		s.Close()
		delete(p.conns, resp.Proposal)
	}
	return err
}

func (p *Provider) disconnect(deal MinerDeal) error {
	s, ok := p.conns[deal.ProposalCid]
	if !ok {
		return nil
	}

	err := s.Close()
	delete(p.conns, deal.ProposalCid)
	return err
}

var _ datatransfer.RequestValidator = &ProviderRequestValidator{}

// ProviderRequestValidator validates data transfer requests for the provider
// in a storage market
type ProviderRequestValidator struct {
	deals *statestore.StateStore
}

// NewProviderRequestValidator returns a new client request validator for the
// given datastore
func NewProviderRequestValidator(deals *statestore.StateStore) *ProviderRequestValidator {
	return &ProviderRequestValidator{
		deals: deals,
	}
}

// ValidatePush validates a push request received from the peer that will send data
// Will succeed only if:
// - voucher has correct type
// - voucher references an active deal
// - referenced deal matches the client
// - referenced deal matches the given base CID
// - referenced deal is in an acceptable state
func (m *ProviderRequestValidator) ValidatePush(
	sender peer.ID,
	voucher datatransfer.Voucher,
	baseCid cid.Cid,
	Selector ipld.Node) error {
	dealVoucher, ok := voucher.(*StorageDataTransferVoucher)
	if !ok {
		return xerrors.Errorf("voucher type %s: %w", voucher.Type(), ErrWrongVoucherType)
	}

	var deal MinerDeal
	err := m.deals.Get(dealVoucher.Proposal).Get(&deal)
	if err != nil {
		return xerrors.Errorf("Proposal CID %s: %w", dealVoucher.Proposal.String(), ErrNoDeal)
	}
	if deal.Client != sender {
		return xerrors.Errorf("Deal Peer %s, Data Transfer Peer %s: %w", deal.Client.String(), sender.String(), ErrWrongPeer)
	}

	if !deal.Ref.Equals(baseCid) {
		return xerrors.Errorf("Deal Payload CID %s, Data Transfer CID %s: %w", string(deal.Proposal.PieceRef), baseCid.String(), ErrWrongPiece)
	}
	for _, state := range DataTransferStates {
		if deal.State == state {
			return nil
		}
	}
	return xerrors.Errorf("Deal State %s: %w", deal.State, ErrInacceptableDealState)
}

// ValidatePull validates a pull request received from the peer that will receive data.
// Will always error because providers should not accept pull requests from a client
// in a storage deal (i.e. send data to client).
func (m *ProviderRequestValidator) ValidatePull(
	receiver peer.ID,
	voucher datatransfer.Voucher,
	baseCid cid.Cid,
	Selector ipld.Node) error {
	return ErrNoPullAccepted
}
