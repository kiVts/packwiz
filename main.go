package main

import (
	// Modules of packwiz
	"github.com/kivts/packwiz/cmd"
	_ "github.com/kivts/packwiz/curseforge"
	_ "github.com/kivts/packwiz/github"
	_ "github.com/kivts/packwiz/migrate"
	_ "github.com/kivts/packwiz/modrinth"
	_ "github.com/kivts/packwiz/settings"
	_ "github.com/kivts/packwiz/url"
	_ "github.com/kivts/packwiz/utils"
)

func main() {
	cmd.Execute()
}
