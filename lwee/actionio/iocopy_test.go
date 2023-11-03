package actionio

import (
	"bytes"
	"crypto/rand"
	"github.com/stretchr/testify/suite"
	"io"
	"pgregory.net/rapid"
	"testing"
)

type ioCopyToMultiWithStatsSuite struct {
	suite.Suite
	copyOptions CopyOptions
	data        []byte
}

func (suite *ioCopyToMultiWithStatsSuite) SetupTest() {
	const defaultDataSize = 10 * 1000 * 1000 // 10MB
	suite.copyOptions = CopyOptions{
		CopyBufferSize: 4 * 1024,
	}
	suite.data = suite.genData(defaultDataSize)
}

func (suite *ioCopyToMultiWithStatsSuite) genData(size int) []byte {
	d := make([]byte, size)
	n, err := rand.Read(d)
	suite.Require().NoErrorf(err, "should not fail")
	suite.Require().Equal(size, n, "should read fill all data")
	return d
}

func (suite *ioCopyToMultiWithStatsSuite) TestOKSingle() {
	suite.copyOptions.CopyBufferSize = 32
	suite.data = suite.genData(suite.copyOptions.CopyBufferSize)
	suite.test()
}

func (suite *ioCopyToMultiWithStatsSuite) TestOK() {
	suite.test()
}

func (suite *ioCopyToMultiWithStatsSuite) test() {
	const readerCount = 5
	// Prepare readers.
	buffers := make([]bytes.Buffer, readerCount)
	bufferWriters := make([]io.Writer, readerCount)
	for i := range buffers {
		bufferWriters[i] = &buffers[i]
	}
	// Write.
	stats, err := ioCopyToMultiWithStats(bytes.NewReader(suite.data), suite.copyOptions, bufferWriters...)
	suite.Require().NoError(err, "copy should not fail")
	suite.Equal(len(suite.data), stats.written, "should have copied all data")
	for i := range buffers {
		suite.Equal(len(suite.data), buffers[i].Len(), "should have copied all data")
		suite.True(bytes.Equal(suite.data, buffers[i].Bytes()), "should have copied correct data")
	}
	suite.Equal(len(suite.data), stats.written, "should log correct written-amount in stats")
}

func (suite *ioCopyToMultiWithStatsSuite) TestProperties() {
	const maxSize = 16 * 1024 * 1024 // 16MB
	rapid.Check(suite.T(), func(t *rapid.T) {
		suite.SetupTest()
		suite.copyOptions = CopyOptions{
			CopyBufferSize: rapid.IntRange(0, maxSize).Draw(t, "copy_buffer_size"),
		}
		suite.data = suite.genData(rapid.IntRange(0, maxSize).Draw(t, "data_size"))
		suite.test()
	})
}

func Test_ioCopyToMultiWithStatsSuite(t *testing.T) {
	suite.Run(t, new(ioCopyToMultiWithStatsSuite))
}
