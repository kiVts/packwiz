package core

import (
	"archive/zip"
	"os"
	"path/filepath"
	"testing"
)

func writeJar(t *testing.T, name string, files map[string]string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	w := zip.NewWriter(f)
	for n, content := range files {
		e, err := w.Create(n)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := e.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestReadJarMetaNeoForge(t *testing.T) {
	// The common case: mods.toml defers the version to the jar manifest.
	path := writeJar(t, "mod.jar", map[string]string{
		"META-INF/neoforge.mods.toml": "modLoader=\"javafml\"\n[[mods]]\nmodId=\"examplemod\"\ndisplayName=\"Example Mod\"\nversion=\"${file.jarVersion}\"\n",
		"META-INF/MANIFEST.MF":        "Manifest-Version: 1.0\r\nImplementation-Version: 1.5.3\r\n\r\n",
	})
	meta, ok, err := ReadJarMeta(path)
	if err != nil || !ok {
		t.Fatalf("expected metadata, got ok=%v err=%v", ok, err)
	}
	if meta.ModID != "examplemod" || meta.Name != "Example Mod" || meta.Version != "1.5.3" {
		t.Fatalf("unexpected metadata: %+v", meta)
	}
}

func TestReadJarMetaForgeLiteralVersion(t *testing.T) {
	path := writeJar(t, "mod.jar", map[string]string{
		"META-INF/mods.toml": "[[mods]]\nmodId=\"other\"\ndisplayName=\"Other Mod\"\nversion=\"2.0.0\"\n",
	})
	meta, ok, err := ReadJarMeta(path)
	if err != nil || !ok {
		t.Fatalf("expected metadata, got ok=%v err=%v", ok, err)
	}
	if meta.Version != "2.0.0" || meta.Name != "Other Mod" {
		t.Fatalf("unexpected metadata: %+v", meta)
	}
}

func TestReadJarMetaFabric(t *testing.T) {
	path := writeJar(t, "mod.jar", map[string]string{
		"fabric.mod.json": `{"id":"fabricmod","name":"Fabric Mod","version":"3.1.4"}`,
	})
	meta, ok, err := ReadJarMeta(path)
	if err != nil || !ok {
		t.Fatalf("expected metadata, got ok=%v err=%v", ok, err)
	}
	if meta.ModID != "fabricmod" || meta.Version != "3.1.4" {
		t.Fatalf("unexpected metadata: %+v", meta)
	}
}

func TestReadJarMetaPlainLibrary(t *testing.T) {
	path := writeJar(t, "lib.jar", map[string]string{"com/example/Thing.class": "not really a class"})
	_, ok, err := ReadJarMeta(path)
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("expected no mod metadata for a plain library jar")
	}
}

func TestNormalizeCategory(t *testing.T) {
	for in, want := range map[string]string{
		"optional": "Optional", " optional ": "Optional", "Optional": "Optional", "": "", "performance mods": "Performance mods",
	} {
		if got := NormalizeCategory(in); got != want {
			t.Errorf("NormalizeCategory(%q) = %q, want %q", in, got, want)
		}
	}
}
