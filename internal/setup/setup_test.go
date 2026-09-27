package setup

import (
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"
)

// golden.json is written by testdata/gen_golden.mjs from the dashboard's TypeScript generators.
type golden struct {
	Functions []struct {
		Fn   string          `json:"fn"`
		Args []any           `json:"args"`
		Out  json.RawMessage `json:"out"`
	} `json:"functions"`
	Generate []struct {
		Name string       `json:"name"`
		Out  goldenResult `json:"out"`
	} `json:"generate"`
}

type goldenFilter struct {
	ID     string `json:"id"`
	Prefix string `json:"prefix"`
	Suffix string `json:"suffix"`
}

type goldenResult struct {
	Roles              []string       `json:"roles"`
	KeyPrefix          string         `json:"key_prefix"`
	Pattern            string         `json:"pattern"`
	BucketPolicy       string         `json:"bucket_policy"`
	ConsumerPolicy     string         `json:"consumer_policy"`
	IAMPolicy          string         `json:"iam_policy"`
	QueuePolicy        string         `json:"queue_policy"`
	TopicPolicy        string         `json:"topic_policy"`
	BucketNotification string         `json:"bucket_notification"`
	Filters            []goldenFilter `json:"filters"`
	SuffixFilters      []string       `json:"suffix_filters"`
	BySuffix           bool           `json:"by_suffix"`
	FiltersNote        string         `json:"filters_note"`
	QueueARN           string         `json:"queue_arn"`
	Commands           []string       `json:"commands"`
}

func readJSON(t *testing.T, path string, v any) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, v); err != nil {
		t.Fatalf("%s: %v", path, err)
	}
}

func boolString(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

func stringsArg(v any) []string {
	if v == nil {
		return nil
	}
	var out []string
	for _, s := range v.([]any) {
		out = append(out, s.(string))
	}
	return out
}

// call runs the Go port of one TypeScript function, with golden.json's argument conventions.
func call(fn string, a []any) (any, error) {
	s := func(i int) string { return a[i].(string) }
	switch fn {
	case "normalizeRoot":
		return NormalizeRoot(s(0)), nil
	case "queueRegion":
		return QueueRegion(s(0)), nil
	case "queueArn":
		return QueueARN(s(0)), nil
	case "topicRegion":
		return TopicRegion(s(0)), nil
	case "arnAccount":
		return ArnAccount(s(0)), nil
	case "isTopicArn":
		return boolString(IsTopicArn(s(0))), nil
	case "s3PolicyTemplate":
		return Stringify(S3PolicyTemplate(s(0), s(1), stringsArg(a[2]), a[3].(bool))), nil
	case "sqsQueueSetup":
		q := SQSQueueSetup(s(0), s(1), s(2), s(3))
		return map[string]string{
			"iam_policy":           Stringify(q.IAMPolicy),
			"queue_policy_for_s3":  Stringify(q.QueuePolicyForS3),
			"queue_policy_for_sns": Stringify(q.QueuePolicyForSNS),
		}, nil
	case "consumerPolicy":
		return Stringify(ConsumerPolicy(s(0))), nil
	case "queueAccessPolicy":
		return Stringify(QueueAccessPolicy(s(0), s(1), s(2))), nil
	case "topicPolicyForS3":
		return Stringify(TopicPolicyForS3(s(0), s(1))), nil
	case "notificationPrefix":
		return NotificationPrefix(s(0), s(1)), nil
	case "patternExtensions":
		return Stringify(PatternExtensions(s(0))), nil
	case "notificationFilters":
		filters, bySuffix, note := NotificationFilters(s(0), s(1))
		list := []any{}
		for _, f := range filters {
			list = append(list, Obj{{"prefix", f.Prefix}, {"suffix", f.Suffix}})
		}
		return Stringify(Obj{{"filters", list}, {"bySuffix", bySuffix}, {"note", note}}), nil
	case "bucketNotification":
		return Stringify(BucketNotification(s(0), s(1), s(2), s(3))), nil
	case "shellQuote":
		return ShellQuote(s(0)), nil
	case "putBucketNotificationCommand":
		return PutBucketNotificationCommand(s(0), BucketNotification(s(1), s(2), s(3), s(4)), s(5)), nil
	case "setQueuePolicyCommand":
		return SetQueuePolicyCommand(s(0), QueueAccessPolicy(s(0), s(1), "")), nil
	case "setTopicPolicyCommand":
		return SetTopicPolicyCommand(s(0), TopicPolicyForS3(s(0), s(1))), nil
	case "snsSubscribeCommand":
		return SnsSubscribeCommand(s(0), s(1), s(2)), nil
	case "outputLoopRisk":
		return boolString(OutputLoopRisk(s(0), s(1))), nil
	}
	return nil, fmt.Errorf("unknown function %q", fn)
}

func TestGoldenFunctions(t *testing.T) {
	var g golden
	readJSON(t, "testdata/golden.json", &g)
	if len(g.Functions) == 0 {
		t.Fatal("no golden function cases")
	}
	for i, c := range g.Functions {
		t.Run(fmt.Sprintf("%02d_%s", i, c.Fn), func(t *testing.T) {
			got, err := call(c.Fn, c.Args)
			if err != nil {
				t.Fatal(err)
			}
			if m, ok := got.(map[string]string); ok {
				var want map[string]string
				if err := json.Unmarshal(c.Out, &want); err != nil {
					t.Fatal(err)
				}
				for k := range want {
					if m[k] != want[k] {
						t.Errorf("%s(%v).%s\n got: %s\nwant: %s", c.Fn, c.Args, k, m[k], want[k])
					}
				}
				return
			}
			var want string
			if err := json.Unmarshal(c.Out, &want); err != nil {
				t.Fatal(err)
			}
			if got != want {
				t.Errorf("%s(%v)\n got: %s\nwant: %s", c.Fn, c.Args, got, want)
			}
		})
	}
}

func TestGoldenGenerate(t *testing.T) {
	var cases struct {
		Generate []struct {
			Name            string `json:"name"`
			Method          string `json:"method"`
			Fanout          string `json:"fanout"`
			Bucket          string `json:"bucket"`
			Region          string `json:"region"`
			Root            string `json:"root"`
			Prefix          string `json:"prefix"`
			Pattern         string `json:"pattern"`
			QueueURL        string `json:"queue_url"`
			TopicARN        string `json:"topic_arn"`
			OutputsToBucket bool   `json:"outputs_to_bucket"`
			DeleteSource    bool   `json:"delete_source"`
		} `json:"generate"`
	}
	readJSON(t, "testdata/cases.json", &cases)
	var g golden
	readJSON(t, "testdata/golden.json", &g)
	if len(cases.Generate) != len(g.Generate) {
		t.Fatalf("cases.json has %d generate cases, golden.json %d: run testdata/gen_golden.mjs", len(cases.Generate), len(g.Generate))
	}
	for i, c := range cases.Generate {
		t.Run(c.Name, func(t *testing.T) {
			r, err := Generate(Input{
				Method: c.Method, Fanout: c.Fanout, Bucket: c.Bucket, Region: c.Region, Root: c.Root,
				Prefix: c.Prefix, Pattern: c.Pattern, QueueURL: c.QueueURL, TopicARN: c.TopicARN,
				OutputsToBucket: c.OutputsToBucket, DeleteSource: c.DeleteSource,
			})
			if err != nil {
				t.Fatal(err)
			}
			got := goldenResult{
				Roles: r.Roles, KeyPrefix: r.KeyPrefix, Pattern: r.Pattern, BucketPolicy: r.BucketPolicy,
				ConsumerPolicy: r.ConsumerPolicy, IAMPolicy: r.IAMPolicy, QueuePolicy: r.QueuePolicy,
				TopicPolicy: r.TopicPolicy, BucketNotification: r.BucketNotification, SuffixFilters: r.SuffixFilters,
				BySuffix: r.BySuffix, FiltersNote: r.FiltersNote, QueueARN: r.QueueARN, Commands: r.Commands,
			}
			for _, f := range r.Filters {
				got.Filters = append(got.Filters, goldenFilter(f))
			}
			want := g.Generate[i].Out
			gv, wv := reflect.ValueOf(got), reflect.ValueOf(want)
			for f := 0; f < gv.NumField(); f++ {
				if !reflect.DeepEqual(gv.Field(f).Interface(), wv.Field(f).Interface()) {
					t.Errorf("%s\n got: %#v\nwant: %#v", gv.Type().Field(f).Name, gv.Field(f).Interface(), wv.Field(f).Interface())
				}
			}
		})
	}
}

func TestQueueARNVariantMatchesURL(t *testing.T) {
	url := "https://sqs.us-east-1.amazonaws.com/123456789012/ingest"
	a := SQSQueueSetup(url, "", "media-in", "arn:aws:sns:us-east-1:123456789012:t")
	b := SQSQueueSetupARN(QueueARN(url), "media-in", "arn:aws:sns:us-east-1:123456789012:t")
	if Stringify(a.IAMPolicy) != Stringify(b.IAMPolicy) || Stringify(a.QueuePolicyForS3) != Stringify(b.QueuePolicyForS3) ||
		Stringify(a.QueuePolicyForSNS) != Stringify(b.QueuePolicyForSNS) {
		t.Fatal("SQSQueueSetupARN differs from SQSQueueSetup for the same queue")
	}
}

func TestGenerateExplicitQueueARN(t *testing.T) {
	r, err := Generate(Input{
		Method: MethodQueue, Bucket: "media-in", Prefix: "incoming/", Pattern: "*.mp4",
		QueueURL: "http://localhost:4566/000000000000/ingest", QueueARN: "arn:aws:sqs:us-east-1:000000000000:ingest",
	})
	if err != nil {
		t.Fatal(err)
	}
	if r.QueueARN != "arn:aws:sqs:us-east-1:000000000000:ingest" {
		t.Errorf("queue ARN %q", r.QueueARN)
	}
	want := `{"QueueConfigurations":[{"Id":"transcdr-mp4","QueueArn":"arn:aws:sqs:us-east-1:000000000000:ingest","Events":["s3:ObjectCreated:*"],"Filter":{"Key":{"FilterRules":[{"Name":"prefix","Value":"incoming/"},{"Name":"suffix","Value":".mp4"}]}}}]}`
	if r.BucketNotification != want {
		t.Errorf("notification\n got: %s\nwant: %s", r.BucketNotification, want)
	}
	if !strings.Contains(r.QueuePolicy, `"aws:SourceAccount":"000000000000"`) || !strings.Contains(r.QueuePolicy, `"arn:aws:s3:::media-in"`) {
		t.Errorf("queue policy %s", r.QueuePolicy)
	}
	if !strings.Contains(r.IAMPolicy, "ConsumeTranscdrTriggers") || !strings.Contains(r.IAMPolicy, "TranscdrObjects") {
		t.Errorf("IAM policy does not hold both the bucket and the queue statements: %s", r.IAMPolicy)
	}
	// A non-AWS URL has no ARN to read.
	if _, err := Generate(Input{Method: MethodQueue, Bucket: "b", QueueURL: "http://localhost:4566/000000000000/ingest"}); err == nil {
		t.Error("a non-AWS queue URL without queue_arn should be refused")
	}
}

func TestGenerateValidation(t *testing.T) {
	for name, in := range map[string]Input{
		"unknown method":      {Method: "poll", Bucket: "b"},
		"unknown fanout":      {Method: MethodQueue, Fanout: "eventbridge", Bucket: "b", QueueURL: "https://sqs.us-east-1.amazonaws.com/123456789012/q"},
		"missing bucket":      {Method: MethodWatch, Bucket: "  "},
		"queue without queue": {Method: MethodQueue, Bucket: "b"},
		"fan-out, no topic":   {Method: MethodQueue, Fanout: FanoutSNS, Bucket: "b", QueueURL: "https://sqs.us-east-1.amazonaws.com/123456789012/q"},
		"bad topic":           {Method: MethodWebhook, Bucket: "b", TopicARN: "arn:aws:sqs:us-east-1:123456789012:q"},
	} {
		if _, err := Generate(in); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}

func TestGenerateWatch(t *testing.T) {
	r, err := Generate(Input{Method: MethodWatch, Bucket: "media-in", Prefix: "incoming/"})
	if err != nil {
		t.Fatal(err)
	}
	if r.Pattern != DefaultPattern {
		t.Errorf("pattern %q", r.Pattern)
	}
	if r.BucketNotification != "" || r.QueuePolicy != "" || r.TopicPolicy != "" || r.ConsumerPolicy != "" || len(r.Commands) != 0 {
		t.Errorf("watch should generate only the bucket policy: %+v", r)
	}
	if r.IAMPolicy != r.BucketPolicy {
		t.Error("watch: the IAM policy is the bucket policy")
	}
	if !reflect.DeepEqual(r.Roles, []string{RoleSource, RoleWatchFolder}) {
		t.Errorf("roles %v", r.Roles)
	}
	if len(r.SuffixFilters) != 10 || r.SuffixFilters[0] != ".mp4" {
		t.Errorf("suffix filters %v", r.SuffixFilters)
	}
}
