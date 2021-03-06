package pieceio_test

import (
	"bytes"
	"context"
	"fmt"
	"github.com/filecoin-project/go-fil-markets/filestore"
	fsmocks "github.com/filecoin-project/go-fil-markets/filestore/mocks"
	"github.com/filecoin-project/go-fil-markets/pieceio"
	"github.com/filecoin-project/go-fil-markets/pieceio/cario"
	pmocks "github.com/filecoin-project/go-fil-markets/pieceio/mocks"
	"github.com/filecoin-project/go-fil-markets/pieceio/padreader"
	"github.com/filecoin-project/go-sectorbuilder"
	dag "github.com/ipfs/go-merkledag"
	dstest "github.com/ipfs/go-merkledag/test"
	ipldfree "github.com/ipld/go-ipld-prime/impl/free"
	"github.com/ipld/go-ipld-prime/traversal/selector"
	"github.com/ipld/go-ipld-prime/traversal/selector/builder"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"io"
	"testing"
)

func Test_ThereAndBackAgain(t *testing.T) {
	tempDir := filestore.Path("./tempDir")
	pr := padreader.NewPadReader()
	cio := cario.NewCarIO()

	store, err := filestore.NewLocalFileStore(tempDir)
	require.NoError(t, err)

	sourceBserv := dstest.Bserv()
	sourceBs := sourceBserv.Blockstore()

	pio := pieceio.NewPieceIO(pr, cio, store, sourceBs)
	require.NoError(t, err)

	dserv := dag.NewDAGService(sourceBserv)
	a := dag.NewRawNode([]byte("aaaa"))
	b := dag.NewRawNode([]byte("bbbb"))
	c := dag.NewRawNode([]byte("cccc"))

	nd1 := &dag.ProtoNode{}
	_ = nd1.AddNodeLink("cat", a)

	nd2 := &dag.ProtoNode{}
	_ = nd2.AddNodeLink("first", nd1)
	_ = nd2.AddNodeLink("dog", b)

	nd3 := &dag.ProtoNode{}
	_ = nd3.AddNodeLink("second", nd2)
	_ = nd3.AddNodeLink("bear", c)

	ctx := context.Background()
	_ = dserv.Add(ctx, a)
	_ = dserv.Add(ctx, b)
	_ = dserv.Add(ctx, c)
	_ = dserv.Add(ctx, nd1)
	_ = dserv.Add(ctx, nd2)
	_ = dserv.Add(ctx, nd3)

	ssb := builder.NewSelectorSpecBuilder(ipldfree.NodeBuilder())
	node := ssb.ExploreFields(func(efsb builder.ExploreFieldsSpecBuilder) {
		efsb.Insert("Links",
			ssb.ExploreIndex(1, ssb.ExploreRecursive(selector.RecursionLimitNone(), ssb.ExploreAll(ssb.ExploreRecursiveEdge()))))
	}).Node()

	bytes, tmpFile, err := pio.GeneratePieceCommitment(nd3.Cid(), node)
	require.NoError(t, err)
	defer func() {
		deferErr := tmpFile.Close()
		require.NoError(t, deferErr)
		deferErr = store.Delete(tmpFile.Path())
		require.NoError(t, deferErr)
	}()
	for _, b := range bytes {
		require.NotEqual(t, 0, b)
	}
	bufSize := int64(16) // small buffer to illustrate the logic
	buf := make([]byte, bufSize)
	var readErr error
	padStart := int64(-1)
	loops := int64(-1)
	read := 0
	skipped, err := tmpFile.Seek(tmpFile.Size()/2, io.SeekStart)
	require.NoError(t, err)
	for readErr == nil {
		loops++
		read, readErr = tmpFile.Read(buf)
		for idx := int64(0); idx < int64(read); idx++ {
			if buf[idx] == 0 {
				if padStart == -1 {
					padStart = skipped + loops*bufSize + idx
				}
			} else {
				padStart = -1
			}
		}
	}
	_, err = tmpFile.Seek(0, io.SeekStart)
	require.NoError(t, err)

	var reader io.Reader
	if padStart != -1 {
		reader = io.LimitReader(tmpFile, padStart)
	} else {
		reader = tmpFile
	}

	id, err := pio.ReadPiece(reader)
	require.NoError(t, err)
	require.Equal(t, nd3.Cid(), id)
}

func Test_StoreRestoreMemoryBuffer(t *testing.T) {
	tempDir := filestore.Path("./tempDir")
	pr := padreader.NewPadReader()
	cio := cario.NewCarIO()

	store, err := filestore.NewLocalFileStore(tempDir)
	require.NoError(t, err)

	sourceBserv := dstest.Bserv()
	sourceBs := sourceBserv.Blockstore()
	pio := pieceio.NewPieceIO(pr, cio, store, sourceBs)

	dserv := dag.NewDAGService(sourceBserv)
	a := dag.NewRawNode([]byte("aaaa"))
	b := dag.NewRawNode([]byte("bbbb"))
	c := dag.NewRawNode([]byte("cccc"))

	nd1 := &dag.ProtoNode{}
	_ = nd1.AddNodeLink("cat", a)

	nd2 := &dag.ProtoNode{}
	_ = nd2.AddNodeLink("first", nd1)
	_ = nd2.AddNodeLink("dog", b)

	nd3 := &dag.ProtoNode{}
	_ = nd3.AddNodeLink("second", nd2)
	_ = nd3.AddNodeLink("bear", c)

	ctx := context.Background()
	_ = dserv.Add(ctx, a)
	_ = dserv.Add(ctx, b)
	_ = dserv.Add(ctx, c)
	_ = dserv.Add(ctx, nd1)
	_ = dserv.Add(ctx, nd2)
	_ = dserv.Add(ctx, nd3)

	ssb := builder.NewSelectorSpecBuilder(ipldfree.NodeBuilder())
	node := ssb.ExploreFields(func(efsb builder.ExploreFieldsSpecBuilder) {
		efsb.Insert("Links",
			ssb.ExploreIndex(1, ssb.ExploreRecursive(selector.RecursionLimitNone(), ssb.ExploreAll(ssb.ExploreRecursiveEdge()))))
	}).Node()

	commitment, tmpFile, err := pio.GeneratePieceCommitment(nd3.Cid(), node)
	require.NoError(t, err)
	defer func() {
		deferErr := tmpFile.Close()
		require.NoError(t, deferErr)
		deferErr = store.Delete(tmpFile.Path())
		require.NoError(t, deferErr)
	}()
	_, err = tmpFile.Seek(0, io.SeekStart)
	require.NoError(t, err)

	for _, b := range commitment {
		require.NotEqual(t, 0, b)
	}
	buf := make([]byte, tmpFile.Size())
	_, err = tmpFile.Read(buf)
	require.NoError(t, err)
	buffer := bytes.NewBuffer(buf)
	secondCommitment, err := sectorbuilder.GeneratePieceCommitment(buffer, uint64(tmpFile.Size()))
	require.NoError(t, err)
	require.Equal(t, commitment, secondCommitment[:])
}

func Test_Failures(t *testing.T) {
	sourceBserv := dstest.Bserv()
	sourceBs := sourceBserv.Blockstore()
	dserv := dag.NewDAGService(sourceBserv)
	a := dag.NewRawNode([]byte("aaaa"))
	b := dag.NewRawNode([]byte("bbbb"))
	c := dag.NewRawNode([]byte("cccc"))

	nd1 := &dag.ProtoNode{}
	_ = nd1.AddNodeLink("cat", a)

	nd2 := &dag.ProtoNode{}
	_ = nd2.AddNodeLink("first", nd1)
	_ = nd2.AddNodeLink("dog", b)

	nd3 := &dag.ProtoNode{}
	_ = nd3.AddNodeLink("second", nd2)
	_ = nd3.AddNodeLink("bear", c)

	ctx := context.Background()
	_ = dserv.Add(ctx, a)
	_ = dserv.Add(ctx, b)
	_ = dserv.Add(ctx, c)
	_ = dserv.Add(ctx, nd1)
	_ = dserv.Add(ctx, nd2)
	_ = dserv.Add(ctx, nd3)

	ssb := builder.NewSelectorSpecBuilder(ipldfree.NodeBuilder())
	node := ssb.ExploreFields(func(efsb builder.ExploreFieldsSpecBuilder) {
		efsb.Insert("Links",
			ssb.ExploreIndex(1, ssb.ExploreRecursive(selector.RecursionLimitNone(), ssb.ExploreAll(ssb.ExploreRecursiveEdge()))))
	}).Node()

	t.Run("create temp file fails", func(t *testing.T) {
		fsmock := fsmocks.FileStore{}
		fsmock.On("CreateTemp").Return(nil, fmt.Errorf("Failed"))
		pio := pieceio.NewPieceIO(nil, nil, &fsmock, sourceBs)
		_, _, err := pio.GeneratePieceCommitment(nd3.Cid(), node)
		require.Error(t, err)
	})
	t.Run("write CAR fails", func(t *testing.T) {
		tempDir := filestore.Path("./tempDir")
		pr := padreader.NewPadReader()
		store, err := filestore.NewLocalFileStore(tempDir)
		require.NoError(t, err)

		ciomock := pmocks.CarIO{}
		any := mock.Anything
		ciomock.On("WriteCar", any, any, any, any, any).Return(fmt.Errorf("failed to write car"))
		pio := pieceio.NewPieceIO(pr, &ciomock, store, sourceBs)
		_, _, err = pio.GeneratePieceCommitment(nd3.Cid(), node)
		require.Error(t, err)
	})
	t.Run("padding fails", func(t *testing.T) {
		pr := padreader.NewPadReader()
		cio := cario.NewCarIO()

		fsmock := fsmocks.FileStore{}
		mockfile := fsmocks.File{}

		fsmock.On("CreateTemp").Return(&mockfile, nil).Once()
		fsmock.On("Delete", mock.Anything).Return(nil).Once()

		counter := 0
		size := 0
		mockfile.On("Write", mock.Anything).Run(func(args mock.Arguments) {
			arg := args[0]
			buf := arg.([]byte)
			size := len(buf)
			counter += size
		}).Return(size, nil).Times(17)
		mockfile.On("Size").Return(int64(484))
		mockfile.On("Write", mock.Anything).Return(0, fmt.Errorf("write failed")).Once()
		mockfile.On("Close").Return(nil).Once()
		mockfile.On("Path").Return(filestore.Path("mock")).Once()

		pio := pieceio.NewPieceIO(pr, cio, &fsmock, sourceBs)
		_, _, err := pio.GeneratePieceCommitment(nd3.Cid(), node)
		require.Error(t, err)
	})
	t.Run("incorrect padding", func(t *testing.T) {
		pr := padreader.NewPadReader()
		cio := cario.NewCarIO()

		fsmock := fsmocks.FileStore{}
		mockfile := fsmocks.File{}

		fsmock.On("CreateTemp").Return(&mockfile, nil).Once()
		fsmock.On("Delete", mock.Anything).Return(nil).Once()

		counter := 0
		size := 0
		mockfile.On("Write", mock.Anything).Run(func(args mock.Arguments) {
			arg := args[0]
			buf := arg.([]byte)
			size := len(buf)
			counter += size
		}).Return(size, nil).Times(17)
		mockfile.On("Size").Return(int64(484))
		mockfile.On("Write", mock.Anything).Return(16, nil).Once()
		mockfile.On("Close").Return(nil).Once()
		mockfile.On("Path").Return(filestore.Path("mock")).Once()

		pio := pieceio.NewPieceIO(pr, cio, &fsmock, sourceBs)
		_, _, err := pio.GeneratePieceCommitment(nd3.Cid(), node)
		require.Error(t, err)
	})
	t.Run("seek fails", func(t *testing.T) {
		pr := padreader.NewPadReader()
		cio := cario.NewCarIO()

		fsmock := fsmocks.FileStore{}
		mockfile := fsmocks.File{}

		fsmock.On("CreateTemp").Return(&mockfile, nil).Once()
		fsmock.On("Delete", mock.Anything).Return(nil).Once()

		counter := 0
		size := 0
		mockfile.On("Write", mock.Anything).Run(func(args mock.Arguments) {
			arg := args[0]
			buf := arg.([]byte)
			size := len(buf)
			counter += size
		}).Return(size, nil).Times(17)
		mockfile.On("Size").Return(int64(484))
		mockfile.On("Write", mock.Anything).Return(24, nil).Once()
		mockfile.On("Close").Return(nil).Once()
		mockfile.On("Path").Return(filestore.Path("mock")).Once()
		mockfile.On("Seek", mock.Anything, mock.Anything).Return(int64(0), fmt.Errorf("seek failed"))

		pio := pieceio.NewPieceIO(pr, cio, &fsmock, sourceBs)
		_, _, err := pio.GeneratePieceCommitment(nd3.Cid(), node)
		require.Error(t, err)
	})
}
