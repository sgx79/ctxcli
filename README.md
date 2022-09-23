environment
===========

**CTX_CONFIG**=~/.ctx.hcl

commands
========

- ctx set [ <**context**> ]
- ctx prompt 
- ctx list
- ctx edit

config
======

```hcl
shell = "" # optional 

context "nomad-db-dev" {

	prompt = "" # optional

	env "NOMAD_TOKEN" {
		type = "static|file|command"
		source = ""
	}

}
```