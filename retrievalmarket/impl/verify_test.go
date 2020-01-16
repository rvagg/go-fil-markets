package retrievalimpl_test

import (
	"context"
	"testing"

	retrievalimpl "github.com/filecoin-project/go-fil-markets/retrievalmarket/impl"
	tut "github.com/filecoin-project/go-fil-markets/shared_testutil"
	blocks "github.com/ipfs/go-block-format"
	"github.com/stretchr/testify/require"
)

func TestSelectorVerifier(t *testing.T) {
	ctx := context.Background()
	testdata := tut.NewTestIPLDTree()

	t.Run("verifies correctly", func(t *testing.T) {
		verifier := retrievalimpl.NewSelectorVerifier(testdata.RootNodeLnk)
		checkVerifySequence(ctx, t, verifier, false, []blocks.Block{
			testdata.RootBlock,
			testdata.LeafAlphaBlock,
			testdata.MiddleMapBlock,
			testdata.LeafAlphaBlock,
			testdata.MiddleListBlock,
			testdata.LeafAlphaBlock,
			testdata.LeafAlphaBlock,
			testdata.LeafBetaBlock,
			testdata.LeafAlphaBlock,
		})
	})
	t.Run("fed incorrect block", func(t *testing.T) {
		t.Run("right away", func(t *testing.T) {
			verifier := retrievalimpl.NewSelectorVerifier(testdata.RootNodeLnk)
			checkVerifySequence(ctx, t, verifier, true, []blocks.Block{
				testdata.LeafAlphaBlock,
			})
		})
		t.Run("in middle", func(t *testing.T) {
			verifier := retrievalimpl.NewSelectorVerifier(testdata.RootNodeLnk)
			checkVerifySequence(ctx, t, verifier, true, []blocks.Block{
				testdata.RootBlock,
				testdata.LeafAlphaBlock,
				testdata.MiddleMapBlock,
				testdata.MiddleListBlock,
			})
		})
		t.Run("at end", func(t *testing.T) {
			verifier := retrievalimpl.NewSelectorVerifier(testdata.RootNodeLnk)
			checkVerifySequence(ctx, t, verifier, true, []blocks.Block{
				testdata.RootBlock,
				testdata.LeafAlphaBlock,
				testdata.MiddleMapBlock,
				testdata.LeafAlphaBlock,
				testdata.MiddleListBlock,
				testdata.LeafAlphaBlock,
				testdata.LeafAlphaBlock,
				testdata.LeafBetaBlock,
				testdata.LeafBetaBlock,
			})
		})
	})

}

func checkVerifySequence(ctx context.Context, t *testing.T, verifier retrievalimpl.BlockVerifier, errorOnLast bool, blks []blocks.Block) {
	for i, b := range blks {
		done, err := verifier.Verify(ctx, b)
		if i < len(blks)-1 {
			require.False(t, done)
			require.NoError(t, err)
		} else {
			if errorOnLast {
				require.False(t, done)
				require.Error(t, err)
			} else {
				require.True(t, done)
				require.NoError(t, err)
			}
		}
	}
}
