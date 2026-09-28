package describe

import (
	"encoding/json"
	"strconv"
	"strings"

	"github.com/branow/repomap/internal/catalog"
	"github.com/branow/repomap/internal/scan"
)

// MaxPaths caps how many paths one prompt carries. Past it the model is
// answering about a file list it cannot hold in view, and the phrases get
// worse rather than more numerous. A repository over the cap still gets its
// sentence; the paths beyond it simply go unannotated, which is the same state
// every obvious file is in anyway.
const MaxPaths = 600

// System is stable across every call in a run, so a provider that
// prefix-caches gets to.
const System = "You index source repositories for a search tool. You answer only " +
	"about what you are shown, and you never speculate about code you were not given."

// Schema is the answer's shape. The fields are `sentence` and `purpose`, never
// `description`: models fill a `description` field with a description of the
// field instead, and each field's text is an ORDER rather than a noun phrase.
// A test pins both.
var Schema = json.RawMessage(`{
  "type": "object",
  "additionalProperties": false,
  "required": ["sentence", "files"],
  "properties": {
    "sentence": {
      "type": "string",
      "description": "Put the finished sentence here verbatim. Never describe what the sentence would say."
    },
    "files": {
      "type": "array",
      "description": "Only files whose purpose the path does not already give away. Leave this empty if there are none.",
      "items": {
        "type": "object",
        "additionalProperties": false,
        "required": ["path", "purpose"],
        "properties": {
          "path": {"type": "string", "description": "The path exactly as it was listed."},
          "purpose": {"type": "string", "description": "Three to six words. Put the finished phrase here verbatim."}
        }
      }
    }
  }
}`)

// Answer is what comes back.
type Answer struct {
	Sentence string       `json:"sentence"`
	Files    []FileAnswer `json:"files"`
}

// FileAnswer is one file the model thought worth a phrase.
type FileAnswer struct {
	Path    string `json:"path"`
	Purpose string `json:"purpose"`
}

// Prompt renders one repository for the model.
//
// It is verb-first with worked examples rather than a list of requirements.
// Listing requirements ("say what it does, and what it is built with") invites
// a model to restate the list; naming the failure mode and showing a good and a
// bad answer does not.
func Prompt(entry catalog.Entry) string {
	var b strings.Builder

	b.WriteString("Repository: " + entry.Repo.Name + "\n")
	if entry.Repo.Summary != "" {
		b.WriteString("The forge's own description: " + entry.Repo.Summary + "\n")
	}
	b.WriteString(counts(entry.Tree) + "\n\n")

	if entry.Tree.Readme != "" {
		b.WriteString("README:\n" + entry.Tree.Readme + "\n\n")
	}
	b.WriteString("Directory structure:\n" + scan.Structure(entry.Tree.Files) + "\n\n")
	b.WriteString("Files:\n" + paths(entry.Tree.Files) + "\n\n")

	b.WriteString(`Write one sentence saying what this repository is and what it is built with.
Name the thing, not the category: "the customer-facing storefront, Next.js and
React, talking to the order service over REST" is useful; "a web application
repository containing source code" is not. Say what it is for, not that it has
files.

Then, for files whose purpose the path does NOT already give away, write a
three-to-six word phrase. Skip README.md, LICENSE, package.json, .gitignore, a
lockfile, a CI config, and anything else whose name is its own explanation - a
phrase restating the path is a phrase the reader has to read past. A repository
where nothing needs one gets an empty list, and that is a correct answer.`)

	return b.String()
}

func counts(tree catalog.Tree) string {
	out := strconv.Itoa(len(tree.Files)) + " tracked files"
	if tree.LastCommit != "" {
		out += ", last commit " + tree.LastCommit
	}
	return out
}

func paths(files []catalog.File) string {
	var b strings.Builder
	for i, file := range files {
		if i == MaxPaths {
			b.WriteString("... and " + strconv.Itoa(len(files)-MaxPaths) + " more\n")
			break
		}
		b.WriteString(file.Path + "\n")
	}
	return strings.TrimRight(b.String(), "\n")
}
