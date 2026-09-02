package cmd

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/kivts/packwiz/core"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var customCmd = &cobra.Command{
	Use:   "custom",
	Short: "Manage mods shipped with the pack instead of downloaded from a mod host",
}

var customAddCmd = &cobra.Command{
	Use:     "add [path to jar]",
	Short:   "Add a local jar to the pack, copying it into custom/",
	Aliases: []string{"install"},
	Args:    cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("Loading modpack...")
		pack, err := core.LoadPack()
		if err != nil {
			fmt.Println(err)
			os.Exit(1)
		}
		index, err := pack.LoadIndex()
		if err != nil {
			fmt.Println(err)
			os.Exit(1)
		}

		sourcePath := args[0]
		sourceInfo, err := os.Stat(sourcePath)
		if err != nil {
			fmt.Printf("Can't read %s: %v\n", sourcePath, err)
			os.Exit(1)
		}
		if sourceInfo.IsDir() {
			fmt.Println("Expected a jar file, not a directory")
			os.Exit(1)
		}

		fileName := filepath.Base(sourcePath)
		packRoot := filepath.Dir(viper.GetString("pack-file"))
		customDir := filepath.Join(packRoot, "custom")
		if err := os.MkdirAll(customDir, 0755); err != nil {
			fmt.Println(err)
			os.Exit(1)
		}
		destPath := filepath.Join(customDir, fileName)

		// Copying onto itself would truncate the jar we're adding.
		if sameFile(sourcePath, destPath) {
			fmt.Printf("Using the jar already in custom/: %s\n", fileName)
		} else {
			if err := copyFile(sourcePath, destPath); err != nil {
				fmt.Println(err)
				os.Exit(1)
			}
			fmt.Printf("Copied %s to custom/%s\n", sourcePath, fileName)
		}

		// The mod's own name and version, read out of the jar - file names are not a
		// reliable source for either.
		name := strings.TrimSuffix(fileName, filepath.Ext(fileName))
		version := ""
		slug := core.SlugifyName(name)
		if meta, ok, err := core.ReadJarMeta(destPath); err != nil {
			fmt.Printf("Warning: couldn't read mod metadata from the jar: %v\n", err)
		} else if ok {
			if meta.Name != "" {
				name = meta.Name
			}
			version = meta.Version
			if meta.ModID != "" {
				slug = core.SlugifyName(meta.ModID)
			}
		}
		if customNameFlag != "" {
			name = customNameFlag
		}
		if customVersionFlag != "" {
			version = customVersionFlag
		}
		if customSlugFlag != "" {
			slug = customSlugFlag
		}

		hash, err := core.HashFile(destPath, "sha256")
		if err != nil {
			fmt.Println(err)
			os.Exit(1)
		}

		side := customSideFlag
		if side == "" {
			side = core.UniversalSide
		}

		modMeta := core.Mod{
			Name:     name,
			FileName: fileName,
			Version:  version,
			Category: core.NormalizeCategory(customCategoryFlag),
			Custom:   true,
			Side:     side,
			Download: core.ModDownload{
				// Leading slash means relative to the pack root, i.e. the jar as
				// served from custom/ - without it the installer would resolve it
				// against the metadata file's folder (mods/custom/...).
				URL:        "/custom/" + fileName,
				HashFormat: "sha256",
				Hash:       hash,
			},
		}
		if customOptionalFlag {
			modMeta.Option = &core.ModOption{
				Optional:    true,
				Description: customOptionalDescFlag,
				Default:     true,
			}
		}

		folder := viper.GetString("meta-folder")
		if folder == "" {
			folder = "mods"
		}
		metaPath := modMeta.SetMetaPath(filepath.Join(viper.GetString("meta-folder-base"), folder, slug+core.MetaExtension))

		format, metaHash, err := modMeta.Write()
		if err != nil {
			fmt.Println(err)
			os.Exit(1)
		}
		if err := index.RefreshFileWithHash(metaPath, format, metaHash, true); err != nil {
			fmt.Println(err)
			os.Exit(1)
		}
		if err := index.Write(); err != nil {
			fmt.Println(err)
			os.Exit(1)
		}
		if err := pack.UpdateIndexHash(); err != nil {
			fmt.Println(err)
			os.Exit(1)
		}
		if err := pack.Write(); err != nil {
			fmt.Println(err)
			os.Exit(1)
		}

		versionSuffix := ""
		if version != "" {
			versionSuffix = " " + version
		}
		category := core.NormalizeCategory(customCategoryFlag)
		if category == "" {
			category = "Required"
		}
		fmt.Printf("Added custom mod %s%s to %s (%s)\n", name, versionSuffix, metaPath, category)
	},
}

func sameFile(a, b string) bool {
	aInfo, err := os.Stat(a)
	if err != nil {
		return false
	}
	bInfo, err := os.Stat(b)
	if err != nil {
		return false
	}
	return os.SameFile(aInfo, bInfo)
}

func copyFile(source, dest string) error {
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dest)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

var customCategoryFlag string
var customNameFlag string
var customVersionFlag string
var customSlugFlag string
var customSideFlag string
var customOptionalFlag bool
var customOptionalDescFlag string

func init() {
	rootCmd.AddCommand(customCmd)
	customCmd.AddCommand(customAddCmd)

	customAddCmd.Flags().StringVarP(&customCategoryFlag, "mod-category", "m", "", "Category to group this mod under in the installer UI (e.g. \"Optional\"); mods without one are required")
	customAddCmd.Flags().StringVar(&customNameFlag, "name", "", "Override the mod name read from the jar")
	customAddCmd.Flags().StringVar(&customVersionFlag, "version", "", "Override the mod version read from the jar")
	customAddCmd.Flags().StringVar(&customSlugFlag, "meta-name", "", "Override the name of the .pw.toml file (defaults to the mod id)")
	customAddCmd.Flags().StringVar(&customSideFlag, "side", "", "The side this mod is installed on (client/server/both, defaults to both)")
	customAddCmd.Flags().BoolVar(&customOptionalFlag, "optional", false, "Mark the mod as optional")
	customAddCmd.Flags().StringVar(&customOptionalDescFlag, "optional-description", "", "Description shown for an optional mod")
}
