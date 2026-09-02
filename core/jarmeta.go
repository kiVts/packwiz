package core

import (
	"archive/zip"
	"encoding/json"
	"io"
	"regexp"
	"strings"

	"github.com/BurntSushi/toml"
)

// JarMeta is the mod identity read out of a jar file.
type JarMeta struct {
	ModID   string
	Name    string
	Version string
}

// jarManifestVersionRe matches the manifest attribute placeholders NeoForge/Forge
// mods use in mods.toml, e.g. version="${file.jarVersion}".
var jarManifestVersionRe = regexp.MustCompile(`^\$\{file\.jarVersion\}$`)

type forgeModsToml struct {
	Mods []struct {
		ModID       string `toml:"modId"`
		DisplayName string `toml:"displayName"`
		Version     string `toml:"version"`
	} `toml:"mods"`
}

type fabricModJson struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Version string `json:"version"`
}

type quiltModJson struct {
	QuiltLoader struct {
		ID       string `json:"id"`
		Version  string `json:"version"`
		Metadata struct {
			Name string `json:"name"`
		} `json:"metadata"`
	} `json:"quilt_loader"`
}

// ReadJarMeta reads a mod's own identity out of a jar: NeoForge/Forge
// (META-INF/neoforge.mods.toml or META-INF/mods.toml), Fabric (fabric.mod.json) or
// Quilt (quilt.mod.json). Returns ok=false when the jar carries no mod metadata,
// which is not an error - a jar can legitimately be a plain library.
func ReadJarMeta(jarPath string) (JarMeta, bool, error) {
	r, err := zip.OpenReader(jarPath)
	if err != nil {
		return JarMeta{}, false, err
	}
	defer r.Close()

	files := make(map[string]*zip.File, len(r.File))
	for _, f := range r.File {
		files[f.Name] = f
	}

	read := func(name string) ([]byte, bool, error) {
		f, ok := files[name]
		if !ok {
			return nil, false, nil
		}
		rc, err := f.Open()
		if err != nil {
			return nil, false, err
		}
		defer rc.Close()
		data, err := io.ReadAll(rc)
		if err != nil {
			return nil, false, err
		}
		return data, true, nil
	}

	// NeoForge / Forge
	for _, name := range []string{"META-INF/neoforge.mods.toml", "META-INF/mods.toml"} {
		data, ok, err := read(name)
		if err != nil {
			return JarMeta{}, false, err
		}
		if !ok {
			continue
		}
		var parsed forgeModsToml
		if _, err := toml.Decode(string(data), &parsed); err != nil {
			return JarMeta{}, false, err
		}
		if len(parsed.Mods) == 0 {
			continue
		}
		mod := parsed.Mods[0]
		version := mod.Version
		// mods.toml usually defers the version to the jar manifest.
		if jarManifestVersionRe.MatchString(version) {
			version = ""
			if manifest, ok, err := read("META-INF/MANIFEST.MF"); err == nil && ok {
				version = manifestAttribute(string(manifest), "Implementation-Version")
			}
		}
		return JarMeta{ModID: mod.ModID, Name: mod.DisplayName, Version: version}, true, nil
	}

	// Fabric
	if data, ok, err := read("fabric.mod.json"); err != nil {
		return JarMeta{}, false, err
	} else if ok {
		var parsed fabricModJson
		if err := json.Unmarshal(data, &parsed); err != nil {
			return JarMeta{}, false, err
		}
		return JarMeta{ModID: parsed.ID, Name: parsed.Name, Version: parsed.Version}, true, nil
	}

	// Quilt
	if data, ok, err := read("quilt.mod.json"); err != nil {
		return JarMeta{}, false, err
	} else if ok {
		var parsed quiltModJson
		if err := json.Unmarshal(data, &parsed); err != nil {
			return JarMeta{}, false, err
		}
		return JarMeta{
			ModID:   parsed.QuiltLoader.ID,
			Name:    parsed.QuiltLoader.Metadata.Name,
			Version: parsed.QuiltLoader.Version,
		}, true, nil
	}

	return JarMeta{}, false, nil
}

// manifestAttribute pulls a single attribute out of a jar manifest. Continuation
// lines (a leading space) are joined onto the previous value, per the manifest spec.
func manifestAttribute(manifest string, key string) string {
	var value string
	found := false
	for _, line := range strings.Split(manifest, "\n") {
		line = strings.TrimRight(line, "\r")
		if found {
			if strings.HasPrefix(line, " ") {
				value += strings.TrimPrefix(line, " ")
				continue
			}
			break
		}
		if strings.HasPrefix(line, key+":") {
			value = strings.TrimSpace(strings.TrimPrefix(line, key+":"))
			found = true
		}
	}
	return value
}
