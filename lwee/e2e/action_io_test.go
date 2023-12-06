package e2e

import (
	"bufio"
	"crypto/md5"
	"fmt"
	"github.com/stretchr/testify/suite"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

type actionIODataIntegritySuite struct {
	suite.Suite
	contextDir string
}

func (suite *actionIODataIntegritySuite) SetupSuite() {
	suite.contextDir = "./action-io/project-data-integrity"
	// Go mod vendor the project action that uses the Go SDK.
	cmd := exec.Command("go", "mod", "vendor")
	cmd.Dir = filepath.Join(suite.contextDir, "actions", "copyGoSDK")
	vendorOut, err := cmd.CombinedOutput()
	if err != nil {
		suite.T().Logf("go mod vendor output:\n%s", vendorOut)
		suite.FailNow("go mod vendor should not fail")
	}
}

func (suite *actionIODataIntegritySuite) TearDownSuite() {
	// Remove vendor directory from project action that uses the Go SDK.
	err := os.RemoveAll(filepath.Join(suite.contextDir, "actions", "copyGoSDK", "vendor"))
	suite.Assert().NoError(err, "remove vendor directory for copyGoSDK action should not fail")
}

func (suite *actionIODataIntegritySuite) test(flowName string) {
	const inputSize = 50 * 1024 * 1024 // 50MB.
	// Generate input data.
	inputFilename := filepath.Join(suite.contextDir, "src/input")
	outputFilename := filepath.Join(suite.contextDir, "out/output")
	f, err := os.OpenFile(inputFilename, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0660)
	suite.Require().NoError(err, "open input file should not fail")
	suite.T().Cleanup(func() {
		if !suite.T().Failed() {
			_ = os.Remove(inputFilename)
			_ = os.Remove(outputFilename)
		}
	})
	defer func() { _ = f.Close() }()
	fBufferedWriter := bufio.NewWriterSize(f, 4*1024*1024) // 4MB.
	inputHash := md5.New()
	w := io.MultiWriter(fBufferedWriter, inputHash)
	v := 0
	for n := 0; n < inputSize; {
		nn, err := fmt.Fprintf(w, "%6d;", v) // Do not use line separators here as stdcopy is very slow with that.
		n += nn
		// Do a manual error test in order to speed up testing because testify assertions
		// are slow.
		if err != nil {
			suite.FailNow("write input file should not fail")
		}
		v++
		if v > 999999 {
			v = 0
		}
	}
	err = fBufferedWriter.Flush()
	suite.Require().NoError(err, "flush input file buffer should not fail")
	err = f.Close()
	suite.Require().NoError(err, "close input file should not fail")

	// Run.
	e2eConfig := config{
		command:      "run",
		contextDir:   suite.contextDir,
		flowFilename: flowName,
	}
	err = run(suite.T(), e2eConfig)
	suite.Require().NoError(err, "should not fail")

	// Calculate MD5 sum of output data.
	f, err = os.Open(outputFilename)
	suite.Require().NoError(err, "open output file should not fail")
	defer func() { _ = f.Close() }()
	outputHash := md5.New()
	_, err = io.Copy(outputHash, f)
	suite.Require().NoError(err, "read output file should not fail")
	suite.Equal(inputHash.Sum(nil), outputHash.Sum(nil), "input and output file should have same md5 hash")
}

func (suite *actionIODataIntegritySuite) TestStdinStdout() {
	suite.test("flow-stdin-stdout.yaml")
}

func (suite *actionIODataIntegritySuite) TestGoSDK() {
	suite.test("flow-go-sdk.yaml")
}

func Test_actionIO_dataIntegrity(t *testing.T) {
	t.Parallel()
	suite.Run(t, new(actionIODataIntegritySuite))
}
