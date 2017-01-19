package main

//go:generate go run ../compileAssets.go -o ../../src/remote/bindata.go -i ../../modules -r ../../ -p remote
//go:generate go run ../generateModuleTypes/main.go -o ../../src/utils/moduleTypes.go -i ../../modules -p utils

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/lfkeitel/inmars/src/remote"
	"github.com/lfkeitel/inmars/src/utils"
)

type stringFlagList []string

func (i *stringFlagList) String() string {
	return "List of hosts"
}

func (i *stringFlagList) Set(value string) error {
	set := strings.Split(value, ",")
	*i = append(*i, set...)
	return nil
}

var (
	// Flags
	hostStatList stringFlagList
	moduleList   stringFlagList
	configFile   string
	outputMode   string
	printShort   bool
)

const (
	remoteBasePath string = "$HOME"
)

func init() {
	flag.Var(&hostStatList, "limit", "Hosts from which to get stats")

	flag.Var(&moduleList, "module", "Modules to run on the hosts")
	flag.Var(&moduleList, "m", "Modules to run on the hosts")

	flag.StringVar(&configFile, "c", "", "Configuration file location")

	flag.StringVar(&outputMode, "o", "plain", "Output mode: plain, json")

	flag.BoolVar(&printShort, "s", false, "Print short output")
}

func main() {
	flag.Parse()

	if configFile == "" {
		configFile = utils.FindConfigFile()
	}

	config, err := utils.NewConfig(configFile)
	if err != nil {
		log.Println(err.Error())
		os.Exit(1)
	}

	if err := remote.LoadPrivateKey(config); err != nil {
		log.Println(err.Error())
		os.Exit(1)
	}

	checkedHosts, err := utils.CheckHosts(config, hostStatList)
	if err != nil {
		log.Println(err.Error())
		os.Exit(1)
	}

	tempFileName, err := remote.GenerateScript(config, moduleList)
	if err != nil {
		os.Remove(tempFileName)
		log.Println(err.Error())
		os.Exit(1)
	}

	if err := remote.UploadScript(config, checkedHosts, tempFileName); err != nil {
		log.Println(err.Error())
		os.Exit(1)
	}

	resps, err := remote.ExecuteScript(config, checkedHosts, tempFileName)
	if err != nil {
		log.Println(err.Error())
		os.Exit(1)
	}

	if !config.Core.KeepTempFiles {
		os.Remove(tempFileName)
	}

	printResults(resps)
}

func printResults(resps []*utils.HostResponse) {
	switch outputMode {
	case "json":
		out, err := json.Marshal(resps)
		if err != nil {
			fmt.Println(err.Error())
			return
		}
		fmt.Println(string(out))
	case "plain":
		fallthrough
	default:
		for _, resp := range resps {
			fmt.Printf("Stats for %s:\n\n", resp.Host.Address)
			resp.Print(printShort)
		}
	}
}
