package conctl

import (
	"strings"
	"testing"
)

func TestMarshalWorkflowOrdered_KeyPriorityAndAlphabeticalRemainder(t *testing.T) {
	raw := []byte(`{
		"name":"Workflow",
		"zzz":"tail",
		"id":"wf-1",
		"metadata":{"tags":["a"]},
		"aaa":"head",
		"nodes":[{"data":{"config":{"zeta":1,"model":"m","alpha":2}},"type":"prompt","id":"n1","position":{"y":2,"x":1}}],
		"edges":[{"target":"b","source":"a","id":"e1"}]
	}`)

	out, err := marshalWorkflowOrdered(raw)
	if err != nil {
		t.Fatalf("marshalWorkflowOrdered: %v", err)
	}
	s := string(out)

	// Workflow priority keys first.
	idPos := strings.Index(s, `"id": "wf-1"`)
	namePos := strings.Index(s, `"name": "Workflow"`)
	nodesPos := strings.Index(s, `"nodes": [`)
	edgesPos := strings.Index(s, `"edges": [`)
	metaPos := strings.Index(s, `"metadata": {`)
	if idPos < 0 || namePos < 0 || nodesPos < 0 || edgesPos < 0 || metaPos < 0 {
		t.Fatalf("expected key sections to exist, output=%s", s)
	}
	if idPos >= namePos || namePos >= nodesPos || nodesPos >= edgesPos || edgesPos >= metaPos {
		t.Fatalf("unexpected workflow key order:\n%s", s)
	}

	// Unknown keys are alphabetical.
	aaaPos := strings.Index(s, `"aaa": "head"`)
	zzzPos := strings.Index(s, `"zzz": "tail"`)
	if aaaPos < 0 || zzzPos < 0 || aaaPos >= zzzPos {
		t.Fatalf("expected unknown keys alphabetical order:\n%s", s)
	}

	// Config key priority before unknown config keys.
	modelPos := strings.Index(s, `"model": "m"`)
	alphaPos := strings.Index(s, `"alpha": 2`)
	zetaPos := strings.Index(s, `"zeta": 1`)
	if modelPos < 0 || alphaPos < 0 || zetaPos < 0 {
		t.Fatalf("expected config keys to exist:\n%s", s)
	}
	if modelPos >= alphaPos || alphaPos >= zetaPos {
		t.Fatalf("unexpected config key order:\n%s", s)
	}
}
