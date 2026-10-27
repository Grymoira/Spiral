package realtime

import (
	"encoding/json"
	"sort"
	"testing"
)

func TestStrictEnvelopeRequiredFieldsAndNulls(t *testing.T) {
	cases := []struct {
		name string
		data string
	}{
		{"auth", `{"type":"auth","roomId":"room","sessionToken":"token"}`},
		{"clock_probe", `{"type":"clock_probe","probeId":"probe","c0":0}`},
		{"track", `{"type":"track","sha256":"` + "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" + `","durationMs":1000,"operationId":"unit-track-1","expectedRevision":1}`},
		{"ready_false", `{"type":"ready","trackId":"test","trackGeneration":1,"ready":false}`},
		{"ready_true", `{"type":"ready","trackId":"test","trackGeneration":1,"ready":true}`},
		{"arm_ack", `{"type":"arm_ack","trackId":"test","trackGeneration":1,"eventId":7}`},
		{"schedule_seek_zero", `{"type":"schedule","action":"seek","mediaMs":0,"operationId":"unit-schedule-1","expectedRevision":1}`},
		{"schedule_pause_zero", `{"type":"schedule","action":"pause","mediaMs":0,"operationId":"unit-pause-1","expectedRevision":1}`},
		{"schedule_play_zero", `{"type":"schedule","action":"play","mediaMs":0,"operationId":"unit-play-1","expectedRevision":1}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			valid, err := strictEnvelope([]byte(tc.data))
			if err != nil {
				t.Fatalf("valid command rejected: %v", err)
			}
			if valid.Type == "ready" && valid.Ready != (tc.name == "ready_true") {
				t.Fatalf("boolean value changed: %+v", valid)
			}
			if valid.Type == "schedule" && valid.MediaMs != 0 {
				t.Fatalf("zero position changed: %+v", valid)
			}

			var raw map[string]json.RawMessage
			if err := json.Unmarshal([]byte(tc.data), &raw); err != nil {
				t.Fatal(err)
			}
			// Snapshot the key set before mutating the map. Go map iteration
			// is intentionally unordered; verify every required field.
			keys := make([]string, 0, len(raw))
			for key := range raw {
				keys = append(keys, key)
			}
			sort.Strings(keys)
			for _, key := range keys {
				original := raw[key]
				delete(raw, key)
				missing, err := json.Marshal(raw)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := strictEnvelope(missing); err == nil {
					t.Errorf("accepted missing %s field: %s", key, missing)
				}
				raw[key] = json.RawMessage("null")
				nullField, err := json.Marshal(raw)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := strictEnvelope(nullField); err == nil {
					t.Errorf("accepted null %s field: %s", key, nullField)
				}
				raw[key] = original
			}
		})
	}
}

func TestStrictEnvelopeRejectsWrongTypesAndExtraneousFields(t *testing.T) {
	invalid := []string{
		`null`,
		`[]`,
		`{"type":false,"roomId":"x","sessionToken":"y"}`,
		`{"type":"auth","roomId":42,"sessionToken":"y"}`,
		`{"type":"auth","roomId":"x","sessionToken":true}`,
		`{"type":"clock_probe","probeId":"x","c0":"0"}`,
		`{"type":"clock_probe","probeId":"x","c0":0.5}`,
		`{"type":"ready","trackId":"a","trackGeneration":"1","ready":true}`,
		`{"type":"ready","trackId":"a","trackGeneration":1,"ready":0}`,
		`{"type":"ready","trackId":"a","trackGeneration":-1,"ready":false}`,
		`{"type":"arm_ack","trackId":"a","trackGeneration":1,"eventId":"7"}`,
		`{"type":"arm_ack","trackId":"a","trackGeneration":1,"eventId":-1}`,
		`{"type":"schedule","action":"seek","mediaMs":"0"}`,
		`{"type":"schedule","action":"seek","mediaMs":false}`,
		`{"type":"schedule","action":"seek","mediaMs":1.5}`,
		`{"type":"track","sha256":"x","durationMs":true}`,
		`{"type":"auth","roomId":"x","sessionToken":"y","mediaMs":0}`,
		`{"type":"schedule","action":"seek","mediaMs":0,"unknown":1}`,
		`{"type":"schedule","action":"seek","mediaMs":0,"mediaMs":1}`,
		`{"type":"schedule","action":"seek","mediaMs":0,"ACTION":"seek"}`,
		`{"type":"schedule","action":"seek","mediaMs":0,"operationId":"unit-schedule-1","expectedRevision":1} true`,
		`{"type":"unknown"}`,
	}
	for _, data := range invalid {
		t.Run(data, func(t *testing.T) {
			if _, err := strictEnvelope([]byte(data)); err == nil {
				t.Fatalf("accepted invalid command: %s", data)
			}
		})
	}
}
