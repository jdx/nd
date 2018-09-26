package lib

import (
	"os"
	"path"
	"testing"

	"github.com/stretchr/testify/assert"
)

var fixtures = path.Join("..", "fixtures")
var example1 = path.Join(fixtures, "1-example")

func TestGetPackage(t *testing.T) {
	pkg, err := ParsePackage(example1)
	must(err)
	assert.Equal(t, pkg.Name, "example")
	assert.Equal(t, pkg.Version, "0.0.0")
}

func TestFetchManifest(t *testing.T) {
	man := FetchManifest("edon-test-a")
	man = FetchManifest("edon-test-a")
	assert.Equal(t, man.Name, "edon-test-a")
	assert.Equal(t, man.Versions["0.0.0"].Dist.Tarball, "https://registry.npmjs.org/edon-test-a/-/edon-test-a-0.0.0.tgz")
}

func checkVersion(t *testing.T, p, version string) {
	pkg, err := ParsePackage(p)
	must(err)
	assert.Equal(t, pkg.Version, version)
}

func TestExample1(t *testing.T) {
	os.RemoveAll(path.Join(example1, "node_modules"))
	Refresh(example1)
	checkVersion(t, path.Join(example1, "node_modules/edon-test-a"), "0.0.1")
	checkVersion(t, path.Join(example1, "node_modules/edon-test-b"), "0.0.1")
	checkVersion(t, path.Join(example1, "node_modules/edon-test-c"), "1.0.3")
	checkVersion(t, path.Join(example1, "node_modules/edon-test-a/node_modules/edon-test-c"), "0.0.0")
	checkVersion(t, path.Join(example1, "node_modules/edon-test-b/node_modules/edon-test-c"), "0.0.0")
}
