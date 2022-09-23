package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/hashicorp/hcl/v2/gohcl"
	"github.com/hashicorp/hcl/v2/hclparse"
	shellwords "github.com/mattn/go-shellwords"
)

const (
	fzfCommand = "fzf"
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
}

type Config struct {
	Shell    *string    `hcl:"shell"`
	Contexts []*Context `hcl:"context,block"`
}

func main() {
	var configFile string
	var help bool

	fs := flag.NewFlagSet("ctx", flag.ExitOnError)
	fs.StringVar(&configFile, "config", os.Getenv("CTX_CONFIG"), "")
	fs.BoolVar(&help, "help", false, "")
	fs.Usage = func() {
		fmt.Println("usage: ctx [set <argment> | prompt | list | edit | help]")
		fmt.Println()
		fmt.Println("  if", fzfCommand, "is installed, no argument is need to set context")
		fmt.Println()
		fs.PrintDefaults()
	}
	if err := fs.Parse(os.Args[1:]); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	if help {
		fs.Usage()
		os.Exit(0)
	}

	if configFile == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			fmt.Println(err)
			os.Exit(1)
		}
		configFile = filepath.Join(home, ".ctx.hcl")
	}

	if _, err := os.Stat(configFile); err != nil {
		fmt.Println(err)
		fmt.Println("if config file not found try `ctx edit` to create it first")
		os.Exit(1)
	}

	parser := hclparse.NewParser()
	f, diag := parser.ParseHCLFile(configFile)
	if diag != nil && diag.HasErrors() {
		fmt.Println(diag.Error())
		os.Exit(1)
	}

	var config Config
	diag = gohcl.DecodeBody(f.Body, nil, &config)
	if diag != nil && diag.HasErrors() {
		fmt.Println(diag.Error())
		os.Exit(1)
	}

	var err error

	if fs.NArg() == 0 {
		err = handleSet(&config, "")
	} else {
		switch fs.Arg(0) {
		case "set":
			var ctxid string
			switch fs.NArg() {
			case 0:
			case 1:
				ctxid = fs.Arg(1)
			default:
				fmt.Println("set only accept 1 argument")
				os.Exit(1)
			}

			err = handleSet(&config, ctxid)
		case "prompt":
			err = handlePrompt(&config)
		case "list":
			err = handleList(&config)
		case "edit":
			err = handleEdit(configFile)
		case "help":
			fs.Usage()
			os.Exit(0)
		default:
			fmt.Println("unknown command")
			os.Exit(1)
		}
	}

	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	os.Exit(0)
}

func handleSet(config *Config, ctxid string) error {
	active := os.Getenv("CTX_ACTIVE")
	if active != "" {
		return fmt.Errorf("an active context is running, leave it first: %s", active)
	}

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

	for _, c := range config.Contexts {
		if c.ID == ctxid {
			return switchContext(config, c)
		}
	}

	return fmt.Errorf("context %s not found", ctxid)
}

func handlePrompt(config *Config) error {
	active := os.Getenv("CTX_ACTIVE")
	if active == "" {
		return nil
	}

	for _, c := range config.Contexts {
		if c.ID == active {
			if c.Prompt != nil {
				fmt.Print(*c.Prompt)
			}
		}
	}

	return nil
}

func handleList(config *Config) error {
	for _, c := range config.Contexts {
		fmt.Println(c.ID)
	}
	return nil
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
	environmentVariables = append(environmentVariables, fmt.Sprintf("CTX_ACTIVE=%s", context.ID))
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
	var resolvType string
	if e.Type == nil {
		resolvType = "static"
	} else {
		resolvType = *e.Type
	}

	switch resolvType {
	case "static":
		return e.Source, nil
	case "file":
		content, err := ioutil.ReadFile(e.Source)
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
		return "", fmt.Errorf("unknown environment resolution type: %s", resolvType)
	}
}
