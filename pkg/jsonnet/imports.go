package jsonnet

import (
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"

	jsonnet "github.com/google/go-jsonnet"
	"github.com/google/go-jsonnet/ast"
	"github.com/google/go-jsonnet/toolutils"
	"github.com/pkg/errors"

	"github.com/grafana/tanka/pkg/jsonnet/jpath"
	"github.com/grafana/tanka/pkg/jsonnet/native"
)

// TransitiveImports returns all recursive imports of an environment
func TransitiveImports(dir string) ([]string, error) {
	dir, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}

	dir, err = filepath.EvalSymlinks(dir)
	if err != nil {
		return nil, err
	}

	entrypoint, err := jpath.Entrypoint(dir)
	if err != nil {
		return nil, err
	}

	sonnet, err := os.ReadFile(entrypoint)
	if err != nil {
		return nil, errors.Wrap(err, "opening file")
	}

	jpath, _, rootDir, err := jpath.Resolve(dir, false)
	if err != nil {
		return nil, errors.Wrap(err, "resolving JPATH")
	}

	vm := jsonnet.MakeVM()
	vm.Importer(NewExtendedImporter(jpath))
	for _, nf := range native.Funcs() {
		vm.NativeFunction(nf)
	}

	node, err := jsonnet.SnippetToAST(filepath.Base(entrypoint), string(sonnet))
	if err != nil {
		return nil, errors.Wrap(err, "creating Jsonnet AST")
	}

	imports := make(map[string]bool)
	if err = importRecursive(imports, vm, node, filepath.Base(entrypoint)); err != nil {
		return nil, err
	}

	paths := make([]string, 0, len(imports)+1)
	for k := range imports {

		// Try to resolve any symlinks; use the original path as a last resort
		p, err := filepath.EvalSymlinks(k)
		if err != nil {
			return nil, errors.Wrap(err, "resolving symlinks")
		}
		paths = append(paths, p)

	}
	paths = append(paths, entrypoint)

	for i := range paths {
		paths[i], _ = filepath.Rel(rootDir, paths[i])

		// Normalize path separators for windows
		paths[i] = filepath.ToSlash(paths[i])
	}
	sort.Strings(paths)

	return paths, nil
}

// importRecursive takes a Jsonnet VM and recursively imports the AST. Every
// found import is added to the `list` string slice, which will ultimately
// contain all recursive imports
func importRecursive(list map[string]bool, vm *jsonnet.VM, node ast.Node, currentPath string) error {
	switch node := node.(type) {
	// we have an `import`
	case *ast.Import:
		p := node.File.Value

		contents, foundAt, err := vm.ImportAST(currentPath, p)
		if err != nil {
			return fmt.Errorf("importing '%s' from '%s': %w", p, currentPath, err)
		}

		abs, _ := filepath.Abs(foundAt)
		if list[abs] {
			return nil
		}

		list[abs] = true

		if err := importRecursive(list, vm, contents, foundAt); err != nil {
			return err
		}

	// we have an `importstr`
	case *ast.ImportStr:
		p := node.File.Value

		foundAt, err := vm.ResolveImport(currentPath, p)
		if err != nil {
			return errors.Wrap(err, "importing string")
		}

		abs, _ := filepath.Abs(foundAt)
		if list[abs] {
			return nil
		}

		list[abs] = true

	// neither `import` nor `importstr`, probably object or similar: try children
	default:
		for _, child := range toolutils.Children(node) {
			if err := importRecursive(list, vm, child, currentPath); err != nil {
				return err
			}
		}
	}
	return nil
}

var fileHashes sync.Map

// getSnippetHash takes a jsonnet snippet and calculates a hash from its content
//   and the content of all of its dependencies.
// File hashes are cached in-memory to optimize multiple executions of this function in a process
func getSnippetHash(vm *jsonnet.VM, path, data string) (string, error) {
	node, _ := jsonnet.SnippetToAST(path, data)
	result := map[string]bool{}
	if err := importRecursive(result, vm, node, path); err != nil {
		return "", err
	}
	fileNames := []string{}
	for file := range result {
		fileNames = append(fileNames, file)
	}
	sort.Strings(fileNames)

	fullHasher := sha256.New()
	fullHasher.Write([]byte(data))
	for _, file := range fileNames {
		var fileHash []byte
		if got, ok := fileHashes.Load(file); ok {
			fileHash = got.([]byte)
		} else {
			bytes, err := os.ReadFile(file)
			if err != nil {
				return "", err
			}
			hash := sha256.New()
			fileHash = hash.Sum(bytes)
			fileHashes.Store(file, fileHash)
		}
		fullHasher.Write(fileHash)
	}

	return base64.URLEncoding.EncodeToString(fullHasher.Sum(nil)), nil
}

func uniqueStringSlice(s []string) []string {
	seen := make(map[string]struct{}, len(s))
	j := 0
	for _, v := range s {
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		s[j] = v
		j++
	}
	return s[:j]
}
