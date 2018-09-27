package lib

import (
	"os"
	"path"
	"testing"

	"github.com/stretchr/testify/assert"
)

var fixtures = path.Join("..", "fixtures")

func checkVersion(t *testing.T, p, version string) {
	pkg, err := ParsePackage(p)
	must(err)
	assert.Equal(t, version, pkg.Version)
}

func Test1Example(t *testing.T) {
	root := path.Join(fixtures, "1-example")
	os.RemoveAll(path.Join(root, "node_modules"))
	Load(root)
	checkVersion(t, path.Join(root, "node_modules/edon-test-a"), "0.0.1")
	checkVersion(t, path.Join(root, "node_modules/edon-test-b"), "0.0.1")
	checkVersion(t, path.Join(root, "node_modules/edon-test-c"), "1.0.3")
	checkVersion(t, path.Join(root, "node_modules/edon-test-a/node_modules/edon-test-c"), "0.0.0")
	checkVersion(t, path.Join(root, "node_modules/edon-test-b/node_modules/edon-test-c"), "0.0.0")
	os.RemoveAll(path.Join(root, "node_modules"))
}

func Test2Circ(t *testing.T) {
	root := path.Join(fixtures, "2-circ")
	os.RemoveAll(path.Join(root, "node_modules"))
	Load(root)
	checkVersion(t, path.Join(root, "node_modules/nd-circ-a"), "1.0.0")
	checkVersion(t, path.Join(root, "node_modules/nd-circ-b"), "1.0.0")
	os.RemoveAll(path.Join(root, "node_modules"))
}
