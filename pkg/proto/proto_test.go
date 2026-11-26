package proto

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestNames checks the defaults, the derived names and the subject rules
func TestNames(t *testing.T) {
	var n Names

	switch {
	case n.JobsSubject() != "bee.jobs":
		t.Errorf("jobs %s", n.JobsSubject())
	case n.DoneSubject() != "bee.done":
		t.Errorf("done %s", n.DoneSubject())
	case n.JobsStream() != "BEE_JOBS":
		t.Errorf("jobs stream %s", n.JobsStream())
	case n.DoneStream() != "BEE_DONE":
		t.Errorf("done stream %s", n.DoneStream())
	case n.Consumer() != "BEE_JOBS":
		t.Errorf("consumer %s", n.Consumer())
	}

	other := Names{Jobs: "bee.hetzner.jobs", Done: "bee.hetzner.done"}
	if other.JobsStream() != "BEE_HETZNER_JOBS" {
		t.Errorf("hetzner stream %s", other.JobsStream())
	}

	bad := []Names{
		{Jobs: "a.*", Done: "b"},
		{Jobs: "a", Done: "a"},
		{Jobs: "a", Done: "a.b"},
		{Jobs: "a..b", Done: "c"},
	}
	for _, x := range bad {
		if x.Validate() == nil {
			t.Errorf("%+v accepted", x)
		}
	}
}

// TestEnvelope checks the typed results of an envelope
func TestEnvelope(t *testing.T) {
	var m Envelope
	err := json.Unmarshal([]byte(`{"kind":"images","id":"p 1","result":{"id":"p 1","tasks":[{"id":"t"}]}}`), &m)
	if err != nil {
		t.Fatal(err)
	}

	res, err := m.Images()
	switch {
	case err != nil:
		t.Fatal(err)
	case len(res.Tasks) != 1:
		t.Errorf("tasks %+v", res.Tasks)
	case res.Error != nil:
		t.Error("a done job has an error")
	}

	// another kind
	_, err = m.Audio()
	if err == nil {
		t.Error("images read as audio")
	}

	// a stream result is a video result
	stream := Envelope{Kind: Stream, Result: []byte(`{"id":"s","error":{"code":"INVALID_JOB","message":"m","retryable":false}}`)}
	vr, err := stream.Video()
	switch {
	case err != nil:
		t.Fatal(err)
	case vr.Error == nil:
		t.Error("no error in the result")
	case vr.Error.Code != "INVALID_JOB":
		t.Errorf("error %v", vr.Error)
	}
}

// TestSubscriber checks the consumer names of the subscriber groups
func TestSubscriber(t *testing.T) {
	var n Names

	name, err := n.Subscriber("billing", Audio)
	switch {
	case err != nil:
		t.Fatal(err)
	case name != "BEE_DONE_BILLING_AUDIO":
		t.Errorf("name %s", name)
	}

	_, err = n.Subscriber("a.b", Audio)
	if err == nil {
		t.Error("a dotted group accepted")
	}
}

// TestEnvelopeID checks that a number id stays the same number
func TestEnvelopeID(t *testing.T) {
	in := `{"kind":"audio","id":9007199254740993,"result":{"id":9007199254740993,"user_meta":{"n":9007199254740993}}}`

	var m Envelope
	err := json.Unmarshal([]byte(in), &m)
	if err != nil {
		t.Fatal(err)
	}

	res, err := m.Audio()
	if err != nil {
		t.Fatal(err)
	}

	out, _ := json.Marshal(res)
	if !strings.Contains(string(out), `"id":9007199254740993`) || !strings.Contains(string(out), `"n":9007199254740993`) {
		t.Errorf("id or meta changed: %s", out)
	}
}
