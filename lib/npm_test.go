package lib

import (
	"os"
	"path"
	"testing"

	"github.com/stretchr/testify/assert"
)

var fixtures = path.Join("..", "fixtures")
var example1 = path.Join(fixtures, "1-example")

func checkVersion(t *testing.T, p, version string) {
	pkg, err := ParsePackage(p)
	must(err)
	assert.Equal(t, version, pkg.Version)
}

func TestExample1(t *testing.T) {
	os.RemoveAll(path.Join(example1, "node_modules"))
	Load(example1)
	checkVersion(t, path.Join(example1, "node_modules/edon-test-a"), "0.0.1")
	checkVersion(t, path.Join(example1, "node_modules/edon-test-b"), "0.0.1")
	checkVersion(t, path.Join(example1, "node_modules/edon-test-c"), "1.0.3")
	checkVersion(t, path.Join(example1, "node_modules/edon-test-a/node_modules/edon-test-c"), "0.0.0")
	checkVersion(t, path.Join(example1, "node_modules/edon-test-b/node_modules/edon-test-c"), "0.0.0")
}
