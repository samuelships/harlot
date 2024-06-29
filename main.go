package main

import (
	"github.com/samuelships/harlot/cli"
	"github.com/samuelships/harlot/utils"
)

func main() {
	utils.InitLogger()
	cli.RunCommand()
}
