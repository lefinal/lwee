package testutil

import (
	"fmt"
	"github.com/lefinal/nulls"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func readAstFile(path string) (*ast.File, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, nil, token.LowestPrec)
	if err != nil {
		return nil, err
	}
	return f, nil
}

// extractStringConstants extracts all constant values for constants with
// the given type and prefix in the file.
func extractStringConstants[T ~string](f *ast.File, constPrefix string) ([]T, error) {
	constants := make([]T, 0)
	for _, decl := range f.Decls {
		genDecl, ok := decl.(*ast.GenDecl)
		if !ok {
			continue
		}
		if genDecl.Tok != token.CONST {
			continue
		}
		for _, spec := range genDecl.Specs {
			valueSpec, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			if len(valueSpec.Names) != 1 {
				continue
			}
			if valueName := valueSpec.Names[0].Name; !strings.HasPrefix(valueName, constPrefix) {
				continue
			}
			valueSpecType, ok := valueSpec.Type.(*ast.Ident)
			if !ok {
				continue
			}
			if valueSpecType.Name != reflect.TypeOf(T("")).Name() {
				continue
			}
			if len(valueSpec.Values) != 1 {
				continue
			}
			valueSpecValue, ok := valueSpec.Values[0].(*ast.BasicLit)
			if !ok {
				continue
			}
			v := valueSpecValue.Value
			v = strings.Trim(v, `"`)
			constants = append(constants, T(v))
		}
	}
	return constants, nil
}

// TestMapperWithConstExtraction tests the given mapper function with constants
// from the given file. All constants are extracted using
// extractStringConstants and then assured that mapping does not fail.
// Finally, an unknown constant is used which is assured to make the mapper
// function return an error.
func TestMapperWithConstExtraction[From ~string, To ~string](t *testing.T, mapperFn func(From) (To, error),
	filepath string, prefixOverwrite nulls.String) {
	f, err := readAstFile(filepath)
	if err != nil {
		require.NoError(t, err, "read ast file", nil)
	}
	testMapperWithConstExtraction(t, mapperFn, []*ast.File{f}, prefixOverwrite)
}

// testMapperWithConstExtraction - see TestMapperWithConstExtraction.
func testMapperWithConstExtraction[From ~string, To ~string](t *testing.T, mapperFn func(From) (To, error),
	files []*ast.File, prefixOverwrite nulls.String) {
	// Extract all constants.
	prefix := reflect.TypeOf(From("")).Name()
	if prefixOverwrite.Valid {
		prefix = prefixOverwrite.String
	}
	constants := make([]From, 0)
	for _, f := range files {
		fConstants, err := extractStringConstants[From](f, prefix)
		require.NoError(t, err, "extracting constants should not fail")
		constants = append(constants, fConstants...)
	}
	require.NotEmptyf(t, constants, "should have found types (searching for type '%s' with prefix '%s')",
		reflect.TypeOf(From("")).Name(), prefix)
	// Test.
	for _, c := range constants {
		c := c
		t.Run(fmt.Sprintf("TestConstant_%s", c), func(t *testing.T) {
			_, err := mapperFn(c)
			assert.NoErrorf(t, err, "mapping should not fail for type %v", c)
		})
	}
	t.Run("TestUnknownConstant", func(t *testing.T) {
		_, err := mapperFn(From(NewUUIDV4().String()))
		assert.Error(t, err, "should fail for unknown type")
	})
}

// TestMapperWithConstExtractionFromDir acts like TestMapperWithConstExtraction
// but reads files from the given directory path.
func TestMapperWithConstExtractionFromDir[From ~string, To ~string](t *testing.T, mapperFn func(From) (To, error),
	dir string, prefixOverwrite nulls.String) {
	files, err := os.ReadDir(dir)
	require.NoError(t, err, "reading files from directory should not fail")
	astFiles := make([]*ast.File, 0, len(files))
	for _, fileInfo := range files {
		f, err := readAstFile(filepath.Join(dir, fileInfo.Name()))
		if err != nil {
			require.NoError(t, err, "read ast file", nil)
		}
		astFiles = append(astFiles, f)
	}
	testMapperWithConstExtraction(t, mapperFn, astFiles, prefixOverwrite)
}
