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

enable custom prompt
====================

```bash
__PS1=$PS1
__update_ps1() {
        PROMPT=$(ctx prompt)
        if [[ -n $PROMPT ]]; then
                PS1=$PROMPT
        else
                PS1=$__PS1
        fi
}

export PROMPT_COMMAND=__update_ps1
```