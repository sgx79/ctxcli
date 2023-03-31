package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/hashicorp/hcl/v2/gohcl"
	"github.com/hashicorp/hcl/v2/hclparse"
	"github.com/mattn/go-shellwords"
)

const (
	fzfCommand   = "fzf"
	ctxActiveEnv = "CTX_ACTIVE"
)

type Environment struct {
	ID     string  `hcl:",label"`
	Type   *string `hcl:"type"`
	Source string  `hcl:"source"`
}

type Context struct {
	ID           string         `hcl:",label"`
	Prompt       *string        `hcl:"prompt"`
	Environments []*Environment `hcl:"env,block"`
	SubContexts  []*Context     `hcl:"context,block"`
}

type Config struct {
	Shell    *string    `hcl:"shell"`
	Contexts []*Context `hcl:"context,block"`
}

func lookup(cfg *Config, path string) *Context {
	if path == "" {
		return nil
	}

	var current *Context
	parts := strings.Split(path, ",")
	parent := cfg.Contexts

	for _, p := range parts {

		found := false
		for _, c := range parent {
			if p == c.ID {
				parent = c.SubContexts
				current = c
				found = true
				break
			}
		}

		if !found {
			return nil
		}
	}

	return current
}

func main() {
	var err error

	var configFile string
	var help bool

	fs := flag.NewFlagSet("ctx", flag.ExitOnError)
	fs.StringVar(&configFile, "config", os.Getenv("CTX_CONFIG"), "")
	fs.BoolVar(&help, "help", false, "")
	fs.Usage = func() {
		fmt.Println("usage: ctx [set <argment> | prompt | list | edit | dump | help]")
		fmt.Println()
		fmt.Println("  if", fzfCommand, "is installed, no argument is need to set context")
		fmt.Println()
		fs.PrintDefaults()
	}
	if err = fs.Parse(os.Args[1:]); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	if help {
		fs.Usage()
		os.Exit(0)
	}

	var config Config

	if fs.NArg() == 0 {
		if err = parseConfig(configFile, &config); err != nil {
			fmt.Println(err)
			os.Exit(1)
		}

		err = handleSet(&config, "")
	} else {
		switch fs.Arg(0) {
		case "set":
			var ctxId string
			switch fs.NArg() {
			case 0:
			case 1:
			case 2:
				ctxId = fs.Arg(1)
			default:
				fmt.Println("set only accept 1 argument")
				os.Exit(1)
			}

			if err = parseConfig(configFile, &config); err != nil {
				fmt.Println(err)
				os.Exit(1)
			}

			err = handleSet(&config, ctxId)
		case "prompt":
			if err = parseConfig(configFile, &config); err != nil {
				os.Exit(0)
			}

			err = nil
			handlePrompt(&config)
		case "list":
			if err = parseConfig(configFile, &config); err != nil {
				fmt.Println(err)
				os.Exit(1)
			}

			err = nil
			handleList(&config)
		case "dump":
			if err = parseConfig(configFile, &config); err != nil {
				fmt.Println(err)
				os.Exit(1)
			}

			var buf []byte
			if buf, err = os.ReadFile(configFile); err != nil {
				fmt.Println(err)
				os.Exit(1)
			}

			fmt.Println(string(buf))
		case "edit":
			if err = parseConfig(configFile, &config); err != nil {
				fmt.Println(err)
				os.Exit(1)
			}

			err = handleEdit(configFile)
		case "help":
			fs.Usage()
			os.Exit(0)
		default:
			ctxId := fs.Arg(0)

			if err = parseConfig(configFile, &config); err != nil {
				fmt.Println(err)
				os.Exit(1)
			}

			err = handleSet(&config, ctxId)
		}
	}

	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	os.Exit(0)
}

func handleSet(config *Config, ctxid string) error {
	if ctxid == "" {
		var err error
		ctxid, err = executeAndReturn([]string{
			fzfCommand, "--ansi", "--no-preview",
		}, append(os.Environ(), fmt.Sprintf("FZF_DEFAULT_COMMAND=%s list", os.Args[0])))
		if err != nil {
			fmt.Println(err)
			os.Exit(1)
		}
	}

	var parent = config.Contexts

	active := os.Getenv(ctxActiveEnv)
	if active != "" {
		ctx := lookup(config, active)
		if ctx == nil {
			return errors.New("internal error, current context not found")
		}

		parent = ctx.SubContexts
	}

	for _, c := range parent {
		if c.ID == ctxid {
			return switchContext(config, c)
		}
	}

	return fmt.Errorf("context %s not found", ctxid)
}

func handlePrompt(config *Config) {
	active := os.Getenv(ctxActiveEnv)
	if active == "" {
		return
	}

	c := lookup(config, active)
	if c == nil {
		return
	}

	if c.Prompt != nil {
		fmt.Print(*c.Prompt)
	}
}

func handleList(config *Config) {
	var parent = config.Contexts

	active := os.Getenv(ctxActiveEnv)
	if active != "" {
		ctx := lookup(config, active)
		if ctx == nil {
			return
		}

		parent = ctx.SubContexts
	}

	for _, c := range parent {
		fmt.Println(c.ID)
	}
}

func handleEdit(configFile string) error {
	editorCommand := os.Getenv("EDITOR")
	return execute([]string{editorCommand, configFile}, os.Environ())
}

func generateEnvironment(context *Context, additionalEnvs []string) ([]string, error) {
	var environmentVariables []string
	environmentVariables = append(environmentVariables, os.Environ()...)
	for _, e := range context.Environments {
		val, err := resolveEnvironment(e)
		if err != nil {
			return nil, err
		}
		environmentVariables = append(environmentVariables, fmt.Sprintf("%s=%s", e.ID, val))
	}
	environmentVariables = append(environmentVariables, additionalEnvs...)

	active := os.Getenv("CTX_ACTIVE")
	if active != "" {
		environmentVariables = append(environmentVariables,
			fmt.Sprintf("CTX_ACTIVE=%s,%s", active, context.ID))
	} else {
		environmentVariables = append(environmentVariables,
			fmt.Sprintf("CTX_ACTIVE=%s", context.ID))
	}

	return environmentVariables, nil
}

func switchContext(config *Config, context *Context) error {
	var shell string

	if config.Shell != nil {
		shell = *config.Shell
	}

	if shell == "" {
		shell = os.Getenv("SHELL")
	}

	if shell == "" {
		return errors.New("can not detect current shell")
	}

	envs, args, err := shellwords.ParseWithEnvs(*config.Shell)
	if err != nil {
		return err
	}

	environmentVariables, err := generateEnvironment(context, envs)
	if err != nil {
		return err
	}

	cmd := exec.Command(args[0], args[1:]...)
	cmd.Env = environmentVariables
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func executeAndReturn(args, envs []string) (string, error) {
	var (
		cmd = exec.Command(args[0], args[1:]...)
		out bytes.Buffer
	)

	cmd.Stdin = os.Stdin
	cmd.Stderr = os.Stderr
	cmd.Stdout = &out
	cmd.Env = envs
	if err := cmd.Run(); err != nil {
		return "", err
	}

	return strings.TrimSpace(out.String()), nil
}

func execute(args, envs []string) error {
	var cmd = exec.Command(args[0], args[1:]...)
	cmd.Stdin = os.Stdin
	cmd.Stderr = os.Stderr
	cmd.Stdout = os.Stdout
	cmd.Env = envs
	return cmd.Run()
}

func resolveEnvironment(e *Environment) (string, error) {
	var resolveType string
	if e.Type == nil {
		resolveType = "static"
	} else {
		resolveType = *e.Type
	}

	switch resolveType {
	case "static":
		return e.Source, nil
	case "file":
		content, err := os.ReadFile(e.Source)
		if err != nil {
			return "", err
		}
		return string(content), nil
	case "command":
		envs, args, err := shellwords.ParseWithEnvs(e.Source)
		if err != nil {
			return "", err
		}
		content, err := executeAndReturn(args, append(os.Environ(), envs...))
		if err != nil {
			return "", err
		}
		return content, nil
	default:
		return "", fmt.Errorf("unknown environment resolution type: %s", resolveType)
	}
}

func parseConfig(configFile string, config *Config) error {
	if configFile == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return err
		}
		configFile = filepath.Join(home, ".ctx.hcl")
	}

	if _, err := os.Stat(configFile); err != nil {
		return err
	}

	parser := hclparse.NewParser()
	f, diag := parser.ParseHCLFile(configFile)
	if diag != nil && diag.HasErrors() {
		return diag
	}

	diag = gohcl.DecodeBody(f.Body, nil, config)
	if diag != nil && diag.HasErrors() {
		return diag
	}

	return nil
}
