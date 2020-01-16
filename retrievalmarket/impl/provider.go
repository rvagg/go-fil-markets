package retrievalimpl

import (
	"context"
	"errors"
	"reflect"
	"sync"

	"github.com/filecoin-project/go-address"
	"github.com/ipfs/go-graphsync/storeutil"
	cidlink "github.com/ipld/go-ipld-prime/linking/cid"

	"github.com/filecoin-project/go-fil-markets/retrievalmarket"
	"github.com/filecoin-project/go-fil-markets/retrievalmarket/impl/blockio"
	"github.com/filecoin-project/go-fil-markets/retrievalmarket/impl/providerstates"
	rmnet "github.com/filecoin-project/go-fil-markets/retrievalmarket/network"
	"github.com/filecoin-project/go-fil-markets/shared/tokenamount"
)

type provider struct {

	// TODO: Replace with RetrievalProviderNode for
	// https://github.com/filecoin-project/go-retrieval-market-project/issues/4
	node                    retrievalmarket.RetrievalProviderNode
	network                 rmnet.RetrievalMarketNetwork
	paymentInterval         uint64
	paymentIntervalIncrease uint64
	paymentAddress          address.Address
	pricePerByte            tokenamount.TokenAmount
	subscribers             []retrievalmarket.ProviderSubscriber
	subscribersLk           sync.RWMutex
}

// NewProvider returns a new retrieval provider
func NewProvider(paymentAddress address.Address, node retrievalmarket.RetrievalProviderNode, network rmnet.RetrievalMarketNetwork) retrievalmarket.RetrievalProvider {
	return &provider{
		node:           node,
		network:        network,
		paymentAddress: paymentAddress,
		pricePerByte:   tokenamount.FromInt(2), // TODO: allow setting
	}
}

// Start begins listening for deals on the given host
func (p *provider) Start() error {
	return p.network.SetDelegate(p)
}

// V0
// SetPricePerByte sets the price per byte a miner charges for retrievals
func (p *provider) SetPricePerByte(price tokenamount.TokenAmount) {
	p.pricePerByte = price
}

// SetPaymentInterval sets the maximum number of bytes a a provider will send before
// requesting further payment, and the rate at which that value increases
// TODO: Implement for https://github.com/filecoin-project/go-retrieval-market-project/issues/7
func (p *provider) SetPaymentInterval(paymentInterval uint64, paymentIntervalIncrease uint64) {
	p.paymentInterval = paymentInterval
	p.paymentIntervalIncrease = paymentIntervalIncrease
}

// unsubscribeAt returns a function that removes an item from the subscribers list by comparing
// their reflect.ValueOf before pulling the item out of the slice.  Does not preserve order.
// Subsequent, repeated calls to the func with the same Subscriber are a no-op.
func (p *provider) unsubscribeAt(sub retrievalmarket.ProviderSubscriber) retrievalmarket.Unsubscribe {
	return func() {
		p.subscribersLk.Lock()
		defer p.subscribersLk.Unlock()
		curLen := len(p.subscribers)
		for i, el := range p.subscribers {
			if reflect.ValueOf(sub) == reflect.ValueOf(el) {
				p.subscribers[i] = p.subscribers[curLen-1]
				p.subscribers = p.subscribers[:curLen-1]
				return
			}
		}
	}
}

func (p *provider) notifySubscribers(evt retrievalmarket.ProviderEvent, ds retrievalmarket.ProviderDealState) {
	p.subscribersLk.RLock()
	defer p.subscribersLk.RUnlock()
	for _, cb := range p.subscribers {
		cb(evt, ds)
	}
}

// SubscribeToEvents listens for events that happen related to client retrievals
// TODO: Implement updates as part of https://github.com/filecoin-project/go-retrieval-market-project/issues/7
func (p *provider) SubscribeToEvents(subscriber retrievalmarket.ProviderSubscriber) retrievalmarket.Unsubscribe {
	p.subscribersLk.Lock()
	p.subscribers = append(p.subscribers, subscriber)
	p.subscribersLk.Unlock()

	return p.unsubscribeAt(subscriber)
}

// V1
func (p *provider) SetPricePerUnseal(price tokenamount.TokenAmount) {
	panic("not implemented")
}

func (p *provider) ListDeals() map[retrievalmarket.ProviderDealID]retrievalmarket.ProviderDealState {
	panic("not implemented")
}

// TODO: Update for https://github.com/filecoin-project/go-retrieval-market-project/issues/8
func (p *provider) HandleQueryStream(stream rmnet.RetrievalQueryStream) {
	defer stream.Close()
	query, err := stream.ReadQuery()
	if err != nil {
		return
	}

	answer := retrievalmarket.QueryResponse{
		Status:                     retrievalmarket.QueryResponseUnavailable,
		PaymentAddress:             p.paymentAddress,
		MinPricePerByte:            p.pricePerByte,
		MaxPaymentInterval:         p.paymentInterval,
		MaxPaymentIntervalIncrease: p.paymentIntervalIncrease,
	}

	size, err := p.node.GetPieceSize(query.PieceCID)

	if err == nil {
		answer.Status = retrievalmarket.QueryResponseAvailable
		// TODO: get price, look for already unsealed ref to reduce work
		answer.Size = uint64(size) // TODO: verify on intermediate
	}

	if err != nil && err != retrievalmarket.ErrNotFound {
		log.Errorf("Retrieval query: GetRefs: %s", err)
		answer.Status = retrievalmarket.QueryResponseError
		answer.Message = err.Error()
	}

	if err := stream.WriteQueryResponse(answer); err != nil {
		log.Errorf("Retrieval query: WriteCborRPC: %s", err)
		return
	}
}

func (p *provider) failDeal(dealState *retrievalmarket.ProviderDealState, err error) {
	dealState.Message = err.Error()
	dealState.Status = retrievalmarket.DealStatusFailed
	p.notifySubscribers(retrievalmarket.ProviderEventError, *dealState)
}

// TODO: Update for https://github.com/filecoin-project/go-retrieval-market-project/issues/7
func (p *provider) HandleDealStream(stream rmnet.RetrievalDealStream) {
	defer stream.Close()
	ctx, cancel := context.WithCancel(context.TODO())
	defer cancel()
	dealState := retrievalmarket.ProviderDealState{
		Status:        retrievalmarket.DealStatusNew,
		TotalSent:     0,
		FundsReceived: tokenamount.FromInt(0),
	}
	p.notifySubscribers(retrievalmarket.ProviderEventOpen, dealState)

	bstore := p.node.SealedBlockstore(func() error {
		return nil // TODO: approve unsealing based on amount paid
	})

	loader := storeutil.LoaderForBlockstore(bstore)

	environment := providerDealEnvironment{p.node, nil, p.pricePerByte, p.paymentInterval, p.paymentIntervalIncrease, stream}

	for {
		var handler providerstates.ProviderHandlerFunc

		switch dealState.Status {
		case retrievalmarket.DealStatusNew:
			handler = providerstates.ReceiveDeal
		case retrievalmarket.DealStatusAccepted, retrievalmarket.DealStatusOngoing, retrievalmarket.DealStatusUnsealing:
			handler = providerstates.SendBlocks
		case retrievalmarket.DealStatusFundsNeeded, retrievalmarket.DealStatusFundsNeededLastPayment:
			handler = providerstates.ProcessPayment
		default:
			p.failDeal(&dealState, errors.New("unexpected deal state"))
			return
		}
		dealModifier := handler(ctx, environment, dealState)
		dealModifier(&dealState)
		if retrievalmarket.IsTerminalStatus(dealState.Status) {
			break
		}
		if environment.br == nil {
			environment.br = blockio.NewSelectorBlockReader(cidlink.Link{Cid: dealState.PayloadCID}, loader)
		}
		p.notifySubscribers(retrievalmarket.ProviderEventProgress, dealState)
	}
	if retrievalmarket.IsTerminalSuccess(dealState.Status) {
		p.notifySubscribers(retrievalmarket.ProviderEventComplete, dealState)
	} else {
		p.notifySubscribers(retrievalmarket.ProviderEventError, dealState)
	}
}

type providerDealEnvironment struct {
	node                       retrievalmarket.RetrievalProviderNode
	br                         blockio.BlockReader
	minPricePerByte            tokenamount.TokenAmount
	maxPaymentInterval         uint64
	maxPaymentIntervalIncrease uint64
	stream                     rmnet.RetrievalDealStream
}

func (pde providerDealEnvironment) Node() retrievalmarket.RetrievalProviderNode {
	return pde.node
}

func (pde providerDealEnvironment) DealStream() rmnet.RetrievalDealStream {
	return pde.stream
}

func (pde providerDealEnvironment) CheckDealParams(pricePerByte tokenamount.TokenAmount, paymentInterval uint64, paymentIntervalIncrease uint64) error {
	if pricePerByte.LessThan(pde.minPricePerByte) {
		return errors.New("Price per byte too low")
	}
	if paymentInterval > pde.maxPaymentInterval {
		return errors.New("Payment interval too large")
	}
	if paymentIntervalIncrease > pde.maxPaymentIntervalIncrease {
		return errors.New("Payment interval increase too large")
	}
	return nil
}

func (pde providerDealEnvironment) NextBlock(ctx context.Context) (retrievalmarket.Block, bool, error) {
	if pde.br == nil {
		return retrievalmarket.Block{}, false, errors.New("Could not read block")
	}
	return pde.br.ReadBlock(ctx)
}
