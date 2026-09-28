package engine

import (
	"strings"
	"testing"
)

// validWorkflowJSON is a schema-faithful v2 workflow payload covering every
// executable node type except ai (kept small for readability; ai is covered
// in TestValidateWorkflowV2Valid).
const validWorkflowJSON = `{
  "schemaVersion": 2,
  "nodes": [
    { "id": "n1", "type": "trigger", "label": "User submits checkout", "params": {}, "position": { "x": 0, "y": 100 } },
    { "id": "n2", "type": "db", "label": "Create order", "params": { "table": "orders", "op": "insert", "data": { "total": 42 } }, "position": { "x": 250, "y": 100 } },
    { "id": "n3", "type": "http", "label": "Charge payment", "params": { "url": "https://api.example.com/charge", "method": "POST", "headers": { "Content-Type": "application/json" } }, "position": { "x": 500, "y": 100 } },
    { "id": "n4", "type": "email", "label": "Send receipt", "params": { "to": "user@example.com", "subject": "Your receipt", "body": "Thanks!" }, "position": { "x": 750, "y": 100 } },
    { "id": "n5", "type": "sleep", "label": "Wait for sync", "params": { "duration": "5 minutes" }, "position": { "x": 1000, "y": 100 } }
  ],
  "edges": [
    { "id": "e1", "source": "n1", "target": "n2" },
    { "id": "e2", "source": "n2", "target": "n3", "condition": { "op": "eq", "lhs": "{{n2.status}}", "rhs": "success" } },
    { "id": "e3", "source": "n3", "target": "n4", "label": "paid" },
    { "id": "e4", "source": "n4", "target": "n5" }
  ]
}`

// mustWorkflow parses and validates raw, fatally failing on any error.
func mustWorkflow(t *testing.T, raw string) WorkflowV2 {
	t.Helper()
	wf, err := ParseWorkflowV2(raw)
	if err != nil {
		t.Fatalf("ParseWorkflowV2: %v", err)
	}
	if err := wf.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	return wf
}

func TestParseWorkflowV2Clean(t *testing.T) {
	wf := mustWorkflow(t, validWorkflowJSON)
	if wf.SchemaVersion != WorkflowSchemaV2 {
		t.Errorf("schemaVersion = %d; want %d", wf.SchemaVersion, WorkflowSchemaV2)
	}
	if len(wf.Nodes) != 5 || len(wf.Edges) != 4 {
		t.Fatalf("nodes=%d edges=%d; want 5, 4", len(wf.Nodes), len(wf.Edges))
	}
	if wf.Nodes[4].Type != NodeTypeSleep {
		t.Errorf("node 4 type = %q; want %q", wf.Nodes[4].Type, NodeTypeSleep)
	}
	if wf.Nodes[4].Params == nil || wf.Nodes[4].Params.Duration != "5 minutes" {
		t.Errorf("sleep duration wrong: %+v", wf.Nodes[4].Params)
	}
	if wf.Nodes[1].Params == nil || wf.Nodes[1].Params.Table != "orders" {
		t.Errorf("db table wrong: %+v", wf.Nodes[1].Params)
	}
}

func TestParseWorkflowV2Tolerant(t *testing.T) {
	cases := []struct {
		name    string
		payload string
	}{
		{"fenced json", "```json\n" + validWorkflowJSON + "\n```"},
		{"fenced bare", "```\n" + validWorkflowJSON + "\n```"},
		{"prose around", "Here is the workflow:\n" + validWorkflowJSON + "\nDone!"},
		{"padded", "\n\n   " + validWorkflowJSON + "   \n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mustWorkflow(t, tc.payload)
		})
	}
}

func TestParseWorkflowV2Empty(t *testing.T) {
	wf, err := ParseWorkflowV2("   ")
	if err == nil {
		t.Fatalf("ParseWorkflowV2 on whitespace succeeded: %+v", wf)
	}
	if !strings.Contains(err.Error(), "empty") {
		t.Errorf("error = %q; want it to mention empty", err.Error())
	}
}

func TestValidateWorkflowV2Valid(t *testing.T) {
	cases := []struct {
		name    string
		payload string
	}{
		{"minimal trigger only", `{"schemaVersion":2,"nodes":[{"id":"n1","type":"trigger","label":"Boot","params":{},"position":{"x":0,"y":0}}],"edges":[]}`},
		{"condition and truthy", `{"schemaVersion":2,"nodes":[{"id":"n1","type":"trigger","params":{}},{"id":"n2","type":"condition","params":{"condOp":"truthy","lhs":"{{n1.output.ok}}"}},{"id":"n3","type":"sleep","params":{"duration":"30 seconds"}}],"edges":[{"id":"e1","source":"n1","target":"n2"},{"id":"e2","source":"n2","target":"n3"}]}`},
		{"ai node with retry timeout and edge condition", `{"schemaVersion":2,"nodes":[{"id":"n1","type":"trigger","params":{}},{"id":"n2","type":"ai","params":{"model":"@cf/meta/llama-3.3-70b-instruct-fp8-fast","prompt":"Summarize."},"position":{"x":0,"y":0},"retry":{"limit":3,"delay":"5 seconds","backoff":"exponential"},"timeout":"2 minutes"}],"edges":[{"id":"e1","source":"n1","target":"n2","condition":{"op":"eq","lhs":"{{n1.status}}","rhs":"success"}}]}`},
		{"full order flow", validWorkflowJSON},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mustWorkflow(t, tc.payload)
		})
	}
}

func TestValidateWorkflowV2Rejections(t *testing.T) {
	cases := []struct {
		name    string
		payload string
		want    string
	}{
		{"missing schemaVersion", `{"nodes":[{"id":"n1","type":"trigger","params":{}}],"edges":[]}`, "schemaVersion"},
		{"wrong schemaVersion", `{"schemaVersion":1,"nodes":[{"id":"n1","type":"trigger","params":{}}],"edges":[]}`, "schemaVersion"},
		{"empty nodes", `{"schemaVersion":2,"nodes":[],"edges":[]}`, "no nodes"},
		{"unknown node type", `{"schemaVersion":2,"nodes":[{"id":"n1","type":"function","params":{}}],"edges":[]}`, "not supported"},
		{"duplicate node id", `{"schemaVersion":2,"nodes":[{"id":"n1","type":"trigger","params":{}},{"id":"n1","type":"db","params":{"table":"t","op":"select"}}],"edges":[]}`, "duplicated"},
		{"no trigger", `{"schemaVersion":2,"nodes":[{"id":"n1","type":"db","params":{"table":"t","op":"select"}}],"edges":[]}`, "one trigger"},
		{"two triggers", `{"schemaVersion":2,"nodes":[{"id":"n1","type":"trigger","params":{}},{"id":"n2","type":"trigger","params":{}}],"edges":[]}`, "one trigger"},
		{"edge unknown source", `{"schemaVersion":2,"nodes":[{"id":"n1","type":"trigger","params":{}}],"edges":[{"id":"e1","source":"n9","target":"n1"}]}`, "unknown node"},
		{"edge unknown target", `{"schemaVersion":2,"nodes":[{"id":"n1","type":"trigger","params":{}}],"edges":[{"id":"e1","source":"n1","target":"n9"}]}`, "unknown node"},
		{"self loop", `{"schemaVersion":2,"nodes":[{"id":"n1","type":"trigger","params":{}}],"edges":[{"id":"e1","source":"n1","target":"n1"}]}`, "self-loop"},
		{"http missing url", `{"schemaVersion":2,"nodes":[{"id":"n1","type":"trigger","params":{}},{"id":"n2","type":"http","params":{"method":"GET"}}],"edges":[{"id":"e1","source":"n1","target":"n2"}]}`, "url"},
		{"http relative url", `{"schemaVersion":2,"nodes":[{"id":"n1","type":"trigger","params":{}},{"id":"n2","type":"http","params":{"url":"api.example.com/x","method":"GET"}}],"edges":[{"id":"e1","source":"n1","target":"n2"}]}`, "absolute"},
		{"http bad method", `{"schemaVersion":2,"nodes":[{"id":"n1","type":"trigger","params":{}},{"id":"n2","type":"http","params":{"url":"https://x.dev","method":"TRACE"}}],"edges":[{"id":"e1","source":"n1","target":"n2"}]}`, "method"},
		{"db missing table", `{"schemaVersion":2,"nodes":[{"id":"n1","type":"trigger","params":{}},{"id":"n2","type":"db","params":{"op":"select"}}],"edges":[{"id":"e1","source":"n1","target":"n2"}]}`, "table"},
		{"db bad op", `{"schemaVersion":2,"nodes":[{"id":"n1","type":"trigger","params":{}},{"id":"n2","type":"db","params":{"table":"t","op":"drop"}}],"edges":[{"id":"e1","source":"n1","target":"n2"}]}`, "op"},
		{"ai missing prompt", `{"schemaVersion":2,"nodes":[{"id":"n1","type":"trigger","params":{}},{"id":"n2","type":"ai","params":{"model":"@cf/x"}}],"edges":[{"id":"e1","source":"n1","target":"n2"}]}`, "prompt"},
		{"email missing subject", `{"schemaVersion":2,"nodes":[{"id":"n1","type":"trigger","params":{}},{"id":"n2","type":"email","params":{"to":"a@b.co"}}],"edges":[{"id":"e1","source":"n1","target":"n2"}]}`, "subject"},
		{"email bad to", `{"schemaVersion":2,"nodes":[{"id":"n1","type":"trigger","params":{}},{"id":"n2","type":"email","params":{"to":"nope","subject":"hi"}}],"edges":[{"id":"e1","source":"n1","target":"n2"}]}`, "email address"},
		{"condition bad op", `{"schemaVersion":2,"nodes":[{"id":"n1","type":"trigger","params":{}},{"id":"n2","type":"condition","params":{"condOp":"maybe","lhs":"{{n1.status}}","rhs":"ok"}}],"edges":[{"id":"e1","source":"n1","target":"n2"}]}`, "condOp"},
		{"condition missing rhs", `{"schemaVersion":2,"nodes":[{"id":"n1","type":"trigger","params":{}},{"id":"n2","type":"condition","params":{"condOp":"eq","lhs":"{{n1.status}}"}}],"edges":[{"id":"e1","source":"n1","target":"n2"}]}`, "rhs"},
		{"sleep bad duration", `{"schemaVersion":2,"nodes":[{"id":"n1","type":"trigger","params":{}},{"id":"n2","type":"sleep","params":{"duration":"soon"}}],"edges":[{"id":"e1","source":"n1","target":"n2"}]}`, "duration"},
		{"sleep no unit", `{"schemaVersion":2,"nodes":[{"id":"n1","type":"trigger","params":{}},{"id":"n2","type":"sleep","params":{"duration":"5"}}],"edges":[{"id":"e1","source":"n1","target":"n2"}]}`, "duration"},
		{"timeout bad format", `{"schemaVersion":2,"nodes":[{"id":"n1","type":"trigger","params":{}},{"id":"n2","type":"sleep","params":{"duration":"1 minute"},"timeout":"fast"}],"edges":[{"id":"e1","source":"n1","target":"n2"}]}`, "timeout"},
		{"retry negative limit", `{"schemaVersion":2,"nodes":[{"id":"n1","type":"trigger","params":{}},{"id":"n2","type":"sleep","params":{"duration":"1 minute"},"retry":{"limit":-1}}],"edges":[{"id":"e1","source":"n1","target":"n2"}]}`, "retry.limit"},
		{"retry limit over max", `{"schemaVersion":2,"nodes":[{"id":"n1","type":"trigger","params":{}},{"id":"n2","type":"sleep","params":{"duration":"1 minute"},"retry":{"limit":10001}}],"edges":[{"id":"e1","source":"n1","target":"n2"}]}`, "retry.limit"},
		{"retry bad backoff", `{"schemaVersion":2,"nodes":[{"id":"n1","type":"trigger","params":{}},{"id":"n2","type":"sleep","params":{"duration":"1 minute"},"retry":{"limit":1,"backoff":"random"}}],"edges":[{"id":"e1","source":"n1","target":"n2"}]}`, "backoff"},
		{"edge condition bad op", `{"schemaVersion":2,"nodes":[{"id":"n1","type":"trigger","params":{}},{"id":"n2","type":"sleep","params":{"duration":"1 minute"}}],"edges":[{"id":"e1","source":"n1","target":"n2","condition":{"op":"maybe","lhs":"{{n1.status}}","rhs":"ok"}}]}`, "condition.op"},
		{"cycle in edges", `{"schemaVersion":2,"nodes":[{"id":"n1","type":"trigger","params":{}},{"id":"n2","type":"db","params":{"table":"t","op":"select"}}],"edges":[{"id":"e1","source":"n1","target":"n2"},{"id":"e2","source":"n2","target":"n1"}]}`, "cycle"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			wf, err := ParseWorkflowV2(tc.payload)
			if err != nil {
				t.Fatalf("ParseWorkflowV2 should succeed for %q: %v", tc.name, err)
			}
			verr := wf.Validate()
			if verr == nil {
				t.Fatalf("Validate succeeded for %q; want error mentioning %q", tc.name, tc.want)
			}
			if !strings.Contains(verr.Error(), tc.want) {
				t.Errorf("Validate error for %q = %q; want it to mention %q", tc.name, verr.Error(), tc.want)
			}
		})
	}
}
