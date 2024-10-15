package main

import (
	"bytes"
	"fmt"
	"io"
	"log"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	flag "github.com/spf13/pflag"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

var (
	kubeconfig  string
	allContexts bool
	stdout      bool
	force       bool
)

func init() {
	flag.BoolVar(&allContexts, "all", false, "explode all contexts into separate files")
	flag.BoolVar(&stdout, "stdout", false, "write exploded contexts to stdout instead of files")
	flag.BoolVarP(&force, "force", "f", false, "force overwriting of destination files. Ignored when --stdout is used")
}

func main() {
	flag.Parse()
	args := flag.Args()

	if !allContexts && len(args) == 0 {
		log.Fatal("must specify context names or --all")
	}

	var loadingRules *clientcmd.ClientConfigLoadingRules
	if len(kubeconfig) > 0 {
		loadingRules = &clientcmd.ClientConfigLoadingRules{ExplicitPath: kubeconfig}
	}
	if loadingRules == nil {
		loadingRules = clientcmd.NewDefaultClientConfigLoadingRules()
	}

	cfg, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, nil).RawConfig()
	if err != nil {
		log.Fatal(err)
	}

	if len(cfg.Contexts) == 0 {
		log.Fatal("no contexts found")
	}

	contexts := make(map[string]bool)
	for ctx := range cfg.Contexts {
		contexts[ctx] = true
	}
	todo := make([]string, 0, len(args))

	// Ensure that all specified contexts are present before writing out any files
	if !allContexts {
		for _, contextName := range args {
			if _, ok := contexts[contextName]; !ok {
				log.Fatal(fmt.Errorf("could not find context %q", contextName))
			}

			todo = append(todo, contextName)
		}
	} else {
		todo = slices.Collect(maps.Keys(cfg.Contexts))
	}

	for _, contextName := range todo {
		cfg, err := explodeContext(&cfg, contextName)
		if err != nil {
			log.Fatal(err)
		}

		if stdout {
			content, err := clientcmd.Write(*cfg)
			if err != nil {
				log.Fatal(err)
			}

			if _, err := io.Copy(os.Stdout, bytes.NewReader(content)); err != nil {
				log.Fatal(err)
			}
		} else {
			path := filepath.Join(clientcmd.RecommendedConfigDir, strings.ReplaceAll(contextName, "/", "_"))

			if _, err = os.Stat(path); err == nil {
				if !force {
					log.Printf("file %q already exists, use --force to overwrite", path)
					continue
				}
			} else if !os.IsNotExist(err) {
				log.Fatal(fmt.Errorf("unable to stat file %q: %w", path, err))
			}

			if err := clientcmd.WriteToFile(*cfg, path); err != nil {
				log.Fatal(err)
			}
		}

	}
}

func explodeContext(inCfg *clientcmdapi.Config, contextName string) (*clientcmdapi.Config, error) {
	context, ok := inCfg.Contexts[contextName]
	if !ok || context == nil {
		return nil, fmt.Errorf("cannot find context %q", contextName)
	}

	outCfg := clientcmdapi.NewConfig()
	outCfg.Contexts[contextName] = context

	server, ok := inCfg.Clusters[context.Cluster]
	if !ok {
		return nil, fmt.Errorf("cannot find server %q", context.Cluster)
	}
	outCfg.Clusters[context.Cluster] = server

	auth, ok := inCfg.AuthInfos[context.AuthInfo]
	if !ok {
		return nil, fmt.Errorf("cannot find authinfo %q", context.AuthInfo)
	}
	outCfg.AuthInfos[context.AuthInfo] = auth

	outCfg.CurrentContext = contextName
	outCfg.Extensions = inCfg.Extensions
	outCfg.Preferences = inCfg.Preferences

	return outCfg, nil
}
