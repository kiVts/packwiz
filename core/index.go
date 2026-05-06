package core

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
	gitignore "github.com/sabhiram/go-gitignore"
	"github.com/spf13/viper"
	"github.com/vbauerster/mpb/v4"
	"github.com/vbauerster/mpb/v4/decor"
)

// Index is a representation of the index.toml file for referencing all the files in a pack.
type Index struct {
	HashFormat string
	Files      IndexFiles
	indexFile  string
	packRoot   string
}

// indexTomlRepresentation is the TOML representation of Index (Files must be converted)
type indexTomlRepresentation struct {
	HashFormat string                       `toml:"hash-format"`
	Files      indexFilesTomlRepresentation `toml:"files"`
}

func hashFile(path, format string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}

	h, err := GetHashImpl(format)
	if err != nil {
		_ = f.Close()
		return "", err
	}
	if _, err := io.Copy(h, f); err != nil {
		_ = f.Close()
		return "", err
	}
	err = f.Close()
	if err != nil {
		return "", err
	}
	return h.HashToString(h.Sum(nil)), nil
}

// LoadIndex attempts to load the index file from a path
func LoadIndex(indexFile string) (Index, error) {
	// Decode as indexTomlRepresentation then convert to Index
	var rep indexTomlRepresentation
	if _, err := toml.DecodeFile(indexFile, &rep); err != nil {
		return Index{}, err
	}
	if len(rep.HashFormat) == 0 {
		rep.HashFormat = "sha256"
	}
	index := Index{
		HashFormat: rep.HashFormat,
		Files:      rep.Files.toMemoryRep(),
		indexFile:  indexFile,
		packRoot:   filepath.Dir(indexFile),
	}
	return index, nil
}

// RemoveFile removes a file from the index, given a file path
func (in *Index) RemoveFile(path string) error {
	relPath, err := in.RelIndexPath(path)
	if err != nil {
		return err
	}
	delete(in.Files, relPath)
	return nil
}

func (in *Index) updateFileHashGiven(path, format, hash string, markAsMetaFile bool) error {
	// Remove format if equal to index hash format
	if in.HashFormat == format {
		format = ""
	}

	// Find in index
	relPath, err := in.RelIndexPath(path)
	if err != nil {
		return err
	}
	in.Files.updateFileEntry(relPath, format, hash, markAsMetaFile)
	return nil
}

// updateFile calculates the hash for a given path and updates it in the index
func (in *Index) updateFile(path string) error {
	var hashString string
	if viper.GetBool("no-internal-hashes") {
		hashString = ""
	} else {
		// Hash usage strategy (may change):
		// Just use SHA256, overwrite existing hash regardless of what it is
		// May update later to continue using the same hash that was already being used
		var err error
		hashString, err = hashFile(path, "sha256")
		if err != nil {
			return err
		}
	}

	markAsMetaFile := false
	// If the file has an extension of pw.toml, set markAsMetaFile to true
	if strings.HasSuffix(filepath.Base(path), MetaExtension) {
		markAsMetaFile = true
	}

	return in.updateFileHashGiven(path, "sha256", hashString, markAsMetaFile)
}

// ResolveIndexPath turns a path from the index into a file path on disk
func (in Index) ResolveIndexPath(p string) string {
	return filepath.Join(in.packRoot, filepath.FromSlash(p))
}

// RelIndexPath turns a file path on disk into a path from the index
func (in Index) RelIndexPath(p string) (string, error) {
	rel, err := filepath.Rel(in.packRoot, p)
	if err != nil {
		return "", err
	}
	return filepath.ToSlash(rel), nil
}

var ignoreDefaults = []string{
	// Defaults (can be overridden with a negating pattern preceded with !)

	// Exclude Git metadata
	".git/**",
	".gitattributes",
	".gitignore",

	// Exclude macOS metadata
	".DS_Store",

	// Exclude exported CurseForge zip files
	"/*.zip",

	// Exclude exported Modrinth packs
	"*.mrpack",

	// Exclude packwiz binaries, if the user puts them in their pack folder
	"packwiz.exe",
	"packwiz", // Note: also excludes packwiz/ as a directory - you can negate this pattern if you want a directory called packwiz
}

func readGitignore(path string) (*gitignore.GitIgnore, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		// TODO: check for read errors (and present them)
		return gitignore.CompileIgnoreLines(ignoreDefaults...), false
	}

	s := strings.Split(string(data), "\n")
	var lines []string
	lines = append(lines, ignoreDefaults...)
	lines = append(lines, s...)
	return gitignore.CompileIgnoreLines(lines...), true
}

// Refresh updates the hashes of all the files in the index, and adds new files to the index
func (in *Index) Refresh() error {
	// TODO: If needed, multithreaded hashing
	// for i := 0; i < runtime.NumCPU(); i++ {}

	// Is case-sensitivity a problem?
	pathPF, _ := filepath.Abs(viper.GetString("pack-file"))
	pathIndex, _ := filepath.Abs(in.indexFile)

	pathIgnore, _ := filepath.Abs(filepath.Join(in.packRoot, ".packwizignore"))
	ignore, ignoreExists := readGitignore(pathIgnore)

	fileListSeen := make(map[string]struct{})
	var fileList []string
	var customJars []string
	addFile := func(p string) {
		if _, ok := fileListSeen[p]; ok {
			return
		}
		fileListSeen[p] = struct{}{}
		fileList = append(fileList, p)
	}

	err := filepath.WalkDir(in.packRoot, func(path string, info os.DirEntry, err error) error {
		if err != nil {
			// TODO: Handle errors on individual files properly
			return err
		}

		// Never ignore pack root itself (gitignore doesn't allow ignoring the root)
		if path == in.packRoot {
			return nil
		}

		if info.IsDir() {
			// Don't traverse ignored directories (consistent with Git handling of ignored dirs)
			if ignore.MatchesPath(path) {
				return fs.SkipDir
			}
			// Don't add directories to the file list
			return nil
		}
		// Exit if the files are the same as the pack/index files
		absPath, _ := filepath.Abs(path)
		if absPath == pathPF || absPath == pathIndex {
			return nil
		}
		if ignoreExists {
			if absPath == pathIgnore {
				return nil
			}
		}
		if ignore.MatchesPath(path) {
			return nil
		}

		addFile(path)
		return nil
	})
	if err != nil {
		return err
	}

	customPath := filepath.Join(in.packRoot, "custom")
	if info, err := os.Stat(customPath); err == nil && info.IsDir() {
		err = filepath.WalkDir(customPath, func(path string, info os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if info.IsDir() {
				return nil
			}
			if strings.EqualFold(filepath.Ext(path), ".jar") {
				customJars = append(customJars, path)
				addFile(path)
			}
			return nil
		})
		if err != nil {
			return err
		}
	}

	if len(customJars) > 0 {
		fmt.Printf("Found %d custom jar(s) in custom/\n", len(customJars))
	}

	progressContainer := mpb.New()
	progress := progressContainer.AddBar(int64(len(fileList)),
		mpb.PrependDecorators(
			// simple name decorator
			decor.Name("Refreshing index..."),
			// decor.DSyncWidth bit enables column width synchronization
			decor.Percentage(decor.WCSyncSpace),
		),
		mpb.AppendDecorators(
			// replace ETA decorator with "done" message, OnComplete event
			decor.OnComplete(
				// ETA decorator with ewma age of 60
				decor.EwmaETA(decor.ET_STYLE_GO, 60), "done",
			),
		),
	)

	for _, v := range fileList {
		start := time.Now()

		err := in.updateFile(v)
		if err != nil {
			return err
		}

		progress.Increment(time.Since(start))
	}
	// Close bar
	progress.SetTotal(int64(len(fileList)), true) // If len = 0, we have to manually set complete to true
	progressContainer.Wait()

	// Check all the files exist, remove them if they don't
	for p, file := range in.Files {
		if !file.markedFound() {
			delete(in.Files, p)
		}
	}

	refreshedCustomJars, err := in.RefreshCustomJarDownloadHashes(customJars)
	if err != nil {
		return err
	}
	if len(customJars) > 0 {
		fmt.Printf("Rehashed %d custom jar metadata file(s)\n", refreshedCustomJars)
	}

	return nil
}

func isInDir(dir, path string) bool {
	rel, err := filepath.Rel(dir, path)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}

func (in Index) findCustomJarForMod(mod Mod, customJars []string) (string, bool) {
	customPath := filepath.Join(in.packRoot, "custom")
	destPath := mod.GetDestFilePath()

	if filepath.Ext(destPath) == ".jar" && isInDir(customPath, destPath) {
		if _, err := os.Stat(destPath); err == nil {
			return destPath, true
		}
	}

	if len(mod.FileName) == 0 {
		return "", false
	}

	filename := filepath.Base(filepath.FromSlash(mod.FileName))
	var matches []string
	for _, jar := range customJars {
		if filepath.Base(jar) == filename {
			matches = append(matches, jar)
		}
	}
	if len(matches) == 1 {
		return matches[0], true
	}

	return "", false
}

// RefreshCustomJarDownloadHashes updates URL metadata hashes from matching local jars in /custom.
func (in *Index) RefreshCustomJarDownloadHashes(customJars []string) (int, error) {
	if len(customJars) == 0 {
		return 0, nil
	}

	mods, err := in.LoadAllMods()
	if err != nil {
		return 0, err
	}

	changed := 0
	for _, mod := range mods {
		if mod.Download.Mode != "" && mod.Download.Mode != ModeURL {
			continue
		}

		jarPath, ok := in.findCustomJarForMod(*mod, customJars)
		if !ok {
			continue
		}
		relJarPath, err := in.RelIndexPath(jarPath)
		if err != nil {
			relJarPath = jarPath
		}

		hashFormat := mod.Download.HashFormat
		if !slices.Contains([]string{"sha1", "sha512", "sha256"}, hashFormat) {
			hashFormat = "sha256"
		}

		hash, err := hashFile(jarPath, hashFormat)
		if err != nil {
			return 0, err
		}

		if mod.Download.HashFormat == hashFormat && mod.Download.Hash == hash {
			fmt.Printf("Custom jar metadata already current: %s -> %s\n", mod.GetFilePath(), relJarPath)
			continue
		}

		mod.Download.HashFormat = hashFormat
		mod.Download.Hash = hash
		metaHashFormat, metaHash, err := mod.Write()
		if err != nil {
			return 0, err
		}
		err = in.RefreshFileWithHash(mod.GetFilePath(), metaHashFormat, metaHash, true)
		if err != nil {
			return 0, err
		}
		fmt.Printf("Rehashed custom jar metadata: %s -> %s\n", mod.GetFilePath(), relJarPath)
		changed++
	}

	return changed, nil
}

// Write saves the index file
func (in Index) Write() error {
	// Convert to indexTomlRepresentation
	rep := indexTomlRepresentation{
		HashFormat: in.HashFormat,
		Files:      in.Files.toTomlRep(),
	}

	// TODO: calculate and provide hash while writing?
	f, err := os.Create(in.indexFile)
	if err != nil {
		return err
	}

	enc := toml.NewEncoder(f)
	// Disable indentation
	enc.Indent = ""
	err = enc.Encode(rep)
	if err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}

// RefreshFileWithHash updates a file in the index, given a file hash and whether it should be marked as metafile or not
func (in *Index) RefreshFileWithHash(path, format, hash string, markAsMetaFile bool) error {
	if viper.GetBool("no-internal-hashes") {
		hash = ""
	}
	return in.updateFileHashGiven(path, format, hash, markAsMetaFile)
}

// FindMod finds a mod in the index and returns its path and whether it has been found
func (in Index) FindMod(modName string) (string, bool) {
	for p, v := range in.Files {
		if v.IsMetaFile() {
			_, fileName := path.Split(p)
			fileTrimmed := strings.TrimSuffix(strings.TrimSuffix(fileName, MetaExtension), MetaExtensionOld)
			if fileTrimmed == modName {
				return in.ResolveIndexPath(p), true
			}
		}
	}
	return "", false
}

// getAllMods finds paths to every metadata file (Mod) in the index
func (in Index) getAllMods() []string {
	var list []string
	for p, v := range in.Files {
		if v.IsMetaFile() {
			list = append(list, in.ResolveIndexPath(p))
		}
	}
	return list
}

// LoadAllMods reads all metadata files into Mod structs
func (in Index) LoadAllMods() ([]*Mod, error) {
	modPaths := in.getAllMods()
	mods := make([]*Mod, len(modPaths))
	for i, v := range modPaths {
		modData, err := LoadMod(v)
		if err != nil {
			return nil, fmt.Errorf("failed to read metadata file %s: %w", v, err)
		}
		mods[i] = &modData
	}
	return mods, nil
}
