// Copyright 2018 The sphinx Authors
// Modified based on go-ethereum, which Copyright (C) 2014 The go-ethereum Authors.
//
// The sphinx is free software: you can redistribute it and/or modify
// it under the terms of the GNU Lesser General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// The sphinx is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the sphinx. If not, see <http://www.gnu.org/licenses/>.

// puppeth is a command to assemble networks configure.
package main

import (
	"math/rand"
	"os"
	"time"

	"github.com/shx-project/sphinx/common/log"
	"gopkg.in/urfave/cli.v1"
)

// main is just a boring entry point to set up the CLI app.
func main() {
	app := cli.NewApp()
	app.Name = "promfile"
	app.Usage = "assemble and maintain private Shx networks"
	app.Flags = []cli.Flag{
		cli.StringFlag{
			Name:  "network",
			Usage: "name of the network to administer",
		},
		cli.IntFlag{
			Name:  "loglevel",
			Value: 4,
			Usage: "log level to emit to the screen",
		},
	}
	app.Action = func(c *cli.Context) error {
		// Set up the logger to print everything and the random generator
		log.Root().SetHandler(log.LvlFilterHandler(log.Lvl(c.Int("loglevel")), log.StreamHandler(os.Stdout, log.TerminalFormat(true))))
		rand.Seed(time.Now().UnixNano())

		// Start the prometh and relinquish control
		makePrometh(c.String("network")).run()
		return nil
	}
	app.Run(os.Args)
}