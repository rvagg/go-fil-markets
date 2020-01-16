package blockio

import (
	"bytes"
	"context"

	blocks "github.com/ipfs/go-block-format"
	"github.com/ipld/go-ipld-prime"
	cidlink "github.com/ipld/go-ipld-prime/linking/cid"

	"github.com/filecoin-project/go-fil-markets/retrievalmarket"
)

// BlockVerifier verifies blocks received are part of a dag, in the order
// the dag is expected to be traversed
type BlockVerifier interface {
	Verify(context.Context, blocks.Block) (done bool, err error)
}

// OptimisticVerifier always verifies blocks
type OptimisticVerifier struct {
}

// Verify always returns no error
func (o *OptimisticVerifier) Verify(context.Context, blocks.Block) (bool, error) {
	// It's probably fine
	return false, nil
}

// SelectorVerifier verifies a traversal of an IPLD data structure by feeding blocks in
// in the order they are traversed in a dag walk
type SelectorVerifier struct {
	root      ipld.Link
	traverser *Traverser
}

// NewSelectorVerifier returns a new selector based block verifier
func NewSelectorVerifier(root ipld.Link) BlockVerifier {
	return &SelectorVerifier{root, nil}
}

// Verify verifies that the given block is the next one needed for the current traversal
// and returns true if the traversal is done
func (sv *SelectorVerifier) Verify(ctx context.Context, blk blocks.Block) (done bool, err error) {
	if sv.traverser == nil {
		sv.traverser = NewTraverser(sv.root)
		sv.traverser.Start(ctx)
	}
	if sv.traverser.IsComplete(ctx) {
		return false, retrievalmarket.ErrVerification
	}
	lnk, _ := sv.traverser.CurrentRequest(ctx)
	c := lnk.(cidlink.Link).Cid
	if !c.Equals(blk.Cid()) {
		sv.traverser.Error(ctx, retrievalmarket.ErrVerification)
		return false, retrievalmarket.ErrVerification
	}
	err = sv.traverser.Advance(ctx, bytes.NewBuffer(blk.RawData()))
	if err != nil {
		return false, err
	}
	return sv.traverser.IsComplete(ctx), nil
}

var _ BlockVerifier = &OptimisticVerifier{}
var _ BlockVerifier = &SelectorVerifier{}
