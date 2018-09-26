package lib

import (
	"github.com/stretchr/testify/assert"
	"testing"
)

func TestGetPackage(t *testing.T) {
	pkg := ParsePackage("../fixtures/1-example")
	assert.Equal(t, pkg.Name, "example")
	assert.Equal(t, pkg.Version, "0.0.0")
}

func TestFetchManifest(t *testing.T) {
	man := FetchManifest("edon-test-a")
	man = FetchManifest("edon-test-a")
	assert.Equal(t, man.Name, "edon-test-a")
	assert.Equal(t, man.Versions["0.0.0"].Dist.Tarball, "https://registry.npmjs.org/edon-test-a/-/edon-test-a-0.0.0.tgz")
}
