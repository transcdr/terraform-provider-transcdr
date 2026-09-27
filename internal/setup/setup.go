// Package setup generates what a bucket automation needs on the AWS side: the IAM policy for
// Transcdr's keys, the queue and topic access policies, the bucket notification (with the pattern
// turned into S3 filters), and ready-to-run AWS CLI commands. Everything here is pure, and a port of
// the dashboard's generators (transcdr-frontend src/lib/automate.ts and wizard.ts): the output is
// byte-for-byte the same, which the golden tests check.
package setup

import (
	"fmt"
	"regexp"
	"strings"
)

// Role is what a storage connection is used for.
const (
	RoleSource      = "source"
	RoleWatchFolder = "watch_folder"
	RoleDestination = "destination"
)

const policyVersion = "2012-10-17"

var (
	queueRegionRe = regexp.MustCompile(`^https://sqs[.-]([a-z0-9-]+)\.amazonaws\.com`)
	queueARNRe    = regexp.MustCompile(`^https://sqs[.-]([a-z0-9-]+)\.amazonaws\.com/(\d{12})/([^/?#]+)`)
	topicRegionRe = regexp.MustCompile(`^arn:aws[\w-]*:sns:([a-z0-9-]+):`)
	arnAccountRe  = regexp.MustCompile(`^arn:aws[\w-]*:[a-z0-9-]+:[a-z0-9-]*:(\d{12}):`)
	topicARNRe    = regexp.MustCompile(`^arn:aws[\w-]*:sns:[a-z0-9-]+:\d{12}:[A-Za-z0-9_-]+(\.fifo)?$`)
	singleExtRe   = regexp.MustCompile(`\*\.([A-Za-z0-9]+)$`)
	groupExtRe    = regexp.MustCompile(`\*\.\{([A-Za-z0-9]+(?:,[A-Za-z0-9]+)*)\}$`)
	shellSafeRe   = regexp.MustCompile(`^[A-Za-z0-9_./:=@%+-]+$`)
)

// NormalizeRoot: `videos/` → `videos/`, `/videos` → `videos/`, "" → "".
func NormalizeRoot(root string) string {
	trimmed := strings.TrimLeft(strings.TrimSpace(root), "/")
	if trimmed != "" && !strings.HasSuffix(trimmed, "/") {
		return trimmed + "/"
	}
	return trimmed
}

// QueueRegion is the region in a queue URL, `https://sqs.<region>.amazonaws.com/…`, or "".
func QueueRegion(queueURL string) string {
	if m := queueRegionRe.FindStringSubmatch(strings.TrimSpace(queueURL)); m != nil {
		return m[1]
	}
	return ""
}

// QueueARN is `arn:aws:sqs:<region>:<account>:<name>` from an AWS queue URL, or "".
func QueueARN(queueURL string) string {
	if m := queueARNRe.FindStringSubmatch(strings.TrimSpace(queueURL)); m != nil {
		return fmt.Sprintf("arn:aws:sqs:%s:%s:%s", m[1], m[2], m[3])
	}
	return ""
}

// TopicRegion is the region in an SNS topic ARN, or "".
func TopicRegion(topicARN string) string {
	if m := topicRegionRe.FindStringSubmatch(strings.TrimSpace(topicARN)); m != nil {
		return m[1]
	}
	return ""
}

// ArnAccount is the account id in an ARN (`arn:aws:sns:us-east-1:123456789012:topic` → `123456789012`), or "".
func ArnAccount(arn string) string {
	if m := arnAccountRe.FindStringSubmatch(strings.TrimSpace(arn)); m != nil {
		return m[1]
	}
	return ""
}

// IsTopicArn reports whether value is an SNS topic ARN.
func IsTopicArn(value string) bool {
	return topicARNRe.MatchString(strings.TrimSpace(value))
}

func strs(values ...string) []any {
	out := make([]any, len(values))
	for i, v := range values {
		out[i] = v
	}
	return out
}

func policy(statements ...any) Obj {
	return Obj{{"Version", policyVersion}, {"Statement", statements}}
}

// ---- Roles and IAM ---------------------------------------------------------------------

// RolesFor is the roles the storage connection needs: Watch lists, Queue and Webhook only read;
// plus writing when outputs go back.
func RolesFor(method string, outputsBack bool) []string {
	roles := []string{RoleSource}
	if method == MethodWatch {
		roles = append(roles, RoleWatchFolder)
	}
	if outputsBack {
		roles = append(roles, RoleDestination)
	}
	return roles
}

// S3PolicyTemplate is the S3 policy Transcdr needs, scoped to a bucket and folder. With nil roles it
// covers every role; otherwise only what those roles need: listing and reading always, writing for a
// destination, and deleting when sources are deleted after success.
func S3PolicyTemplate(bucket, root string, roles []string, deleteSource bool) Obj {
	b := bucket
	if b == "" {
		b = "<your-bucket>"
	}
	prefix := NormalizeRoot(root)
	write := roles == nil || contains(roles, RoleDestination)
	remove := write || deleteSource
	objectActions := []string{"s3:GetObject"}
	if write {
		objectActions = append(objectActions, "s3:PutObject")
	}
	if remove {
		objectActions = append(objectActions, "s3:DeleteObject")
	}
	if write {
		objectActions = append(objectActions, "s3:AbortMultipartUpload")
	}
	list := Obj{
		{"Sid", "TranscdrList"},
		{"Effect", "Allow"},
		{"Action", strs("s3:ListBucket", "s3:GetBucketLocation")},
		{"Resource", "arn:aws:s3:::" + b},
	}
	if prefix != "" {
		list = append(list, KV{"Condition", Obj{{"StringLike", Obj{{"s3:prefix", strs(prefix + "*")}}}}})
	}
	objects := Obj{
		{"Sid", "TranscdrObjects"},
		{"Effect", "Allow"},
		{"Action", strs(objectActions...)},
		{"Resource", fmt.Sprintf("arn:aws:s3:::%s/%s*", b, prefix)},
	}
	return policy(list, objects)
}

// QueueSetup is what an SQS trigger queue needs: Transcdr's consumer policy, and the queue access
// policies that let S3 or SNS send to the queue.
type QueueSetup struct {
	ARN               string
	IAMPolicy         Obj
	QueuePolicyForS3  Obj
	QueuePolicyForSNS Obj
}

// SQSQueueSetup mirrors the check's setup for a queue URL. The bucket and topic fill in the queue
// policies' conditions when they are known; region only fills a placeholder ARN for a URL that is
// not an AWS queue URL.
func SQSQueueSetup(queueURL, region, bucket, topicARN string) QueueSetup {
	arn := QueueARN(queueURL)
	if arn == "" {
		r := region
		if r == "" {
			r = "<region>"
		}
		arn = fmt.Sprintf("arn:aws:sqs:%s:<account-id>:<queue>", r)
	}
	return SQSQueueSetupARN(arn, bucket, topicARN)
}

// SQSQueueSetupARN is SQSQueueSetup for a known queue ARN, e.g. a queue whose URL is not an AWS one.
func SQSQueueSetupARN(arn, bucket, topicARN string) QueueSetup {
	parts := strings.Split(arn, ":")
	part := func(i int) string {
		if i < len(parts) {
			return parts[i]
		}
		return ""
	}
	arnRegion, account := part(3), part(4)
	b := strings.TrimSpace(bucket)
	if b == "" {
		b = "YOUR-BUCKET"
	}
	topic := strings.TrimSpace(topicARN)
	if topic == "" {
		topic = fmt.Sprintf("arn:aws:sns:%s:%s:YOUR-TOPIC", arnRegion, account)
	}
	return QueueSetup{
		ARN: arn,
		IAMPolicy: policy(Obj{
			{"Sid", "ConsumeTranscdrTriggers"},
			{"Effect", "Allow"},
			{"Action", strs("sqs:ReceiveMessage", "sqs:DeleteMessage", "sqs:ChangeMessageVisibility", "sqs:GetQueueAttributes")},
			{"Resource", arn},
		}),
		QueuePolicyForS3: policy(Obj{
			{"Sid", "S3SendsObjectEvents"},
			{"Effect", "Allow"},
			{"Principal", Obj{{"Service", "s3.amazonaws.com"}}},
			{"Action", "sqs:SendMessage"},
			{"Resource", arn},
			{"Condition", Obj{
				{"ArnLike", Obj{{"aws:SourceArn", "arn:aws:s3:::" + b}}},
				{"StringEquals", Obj{{"aws:SourceAccount", account}}},
			}},
		}),
		QueuePolicyForSNS: policy(Obj{
			{"Sid", "SnsFansOutObjectEvents"},
			{"Effect", "Allow"},
			{"Principal", Obj{{"Service", "sns.amazonaws.com"}}},
			{"Action", "sqs:SendMessage"},
			{"Resource", arn},
			{"Condition", Obj{{"ArnEquals", Obj{{"aws:SourceArn", topic}}}}},
		}),
	}
}

// ConsumerPolicy is Transcdr's consumer policy on the queue: receive, delete, change visibility,
// read attributes.
func ConsumerPolicy(queueURL string) Obj {
	return SQSQueueSetup(queueURL, "", "", "").IAMPolicy
}

// MergePolicies is one policy document holding every statement of several, for one IAM user with
// shared keys.
func MergePolicies(policies ...Obj) Obj {
	statements := []any{}
	for _, p := range policies {
		if list, ok := p.Get("Statement").([]any); ok {
			statements = append(statements, list...)
		}
	}
	return Obj{{"Version", policyVersion}, {"Statement", statements}}
}

// QueueAccessPolicy is the queue access policy: S3 (straight from the bucket, when topicARN is "")
// or the topic (fan-out) may send to the queue.
func QueueAccessPolicy(queueURL, bucket, topicARN string) Obj {
	if topicARN != "" {
		return SQSQueueSetup(queueURL, "", "", topicARN).QueuePolicyForSNS
	}
	return SQSQueueSetup(queueURL, "", bucket, "").QueuePolicyForS3
}

// TopicPolicyForS3 is the topic policy that lets S3 publish the bucket's events to the topic.
func TopicPolicyForS3(topicARN, bucket string) Obj {
	condition := Obj{{"ArnLike", Obj{{"aws:SourceArn", "arn:aws:s3:::" + bucket}}}}
	if account := ArnAccount(topicARN); account != "" {
		condition = append(condition, KV{"StringEquals", Obj{{"aws:SourceAccount", account}}})
	}
	return policy(Obj{
		{"Sid", "S3PublishesObjectEvents"},
		{"Effect", "Allow"},
		{"Principal", Obj{{"Service", "s3.amazonaws.com"}}},
		{"Action", "sns:Publish"},
		{"Resource", topicARN},
		{"Condition", condition},
	})
}

// ---- Notification filters -----------------------------------------------------------------

// NotificationPrefix is the key prefix a bucket event carries: the connection's folder, then the
// automation's prefix.
func NotificationPrefix(root, prefix string) string {
	return NormalizeRoot(root) + NormalizeRoot(prefix)
}

// PatternExtensions is the extensions a pattern ends in: `*.mp4` → [mp4], `*.{mp4,mov}` → [mp4, mov],
// anything else → [].
func PatternExtensions(pattern string) []string {
	p := strings.TrimSpace(pattern)
	if m := singleExtRe.FindStringSubmatch(p); m != nil {
		return []string{m[1]}
	}
	if m := groupExtRe.FindStringSubmatch(p); m != nil {
		var out []string
		for _, ext := range strings.Split(m[1], ",") {
			if !contains(out, ext) {
				out = append(out, ext)
			}
		}
		return out
	}
	return []string{}
}

// NotificationFilter is one notification configuration's filter.
type NotificationFilter struct {
	// Prefix is the key prefix, or "" for none.
	Prefix string
	// Suffix is `.mp4`, or "" for none.
	Suffix string
}

// ID is the configuration id the dashboard gives it: `transcdr-mp4`, or `transcdr` without a suffix.
func (f NotificationFilter) ID() string {
	if f.Suffix != "" {
		return "transcdr-" + f.Suffix[1:]
	}
	return "transcdr"
}

// NotificationFilters: S3 filters take one prefix and one suffix per configuration, so `*.ext`
// becomes a suffix, `*.{a,b,c}` one configuration per extension with the same prefix, anything else
// the prefix only. BySuffix is false in that last case; Note is what to tell the customer.
func NotificationFilters(keyPrefix, pattern string) (filters []NotificationFilter, bySuffix bool, note string) {
	exts := PatternExtensions(pattern)
	if len(exts) == 0 {
		shown := strings.TrimSpace(pattern)
		if shown == "" {
			shown = "(none)"
		}
		every := ""
		if keyPrefix == "" {
			every = " (here: every new object)"
		}
		return []NotificationFilter{{Prefix: keyPrefix}}, false,
			fmt.Sprintf("The pattern %s has no simple suffix, so the notification filters on the prefix only%s. The automation's pattern skips the other files.", shown, every)
	}
	for _, ext := range exts {
		filters = append(filters, NotificationFilter{Prefix: keyPrefix, Suffix: "." + ext})
	}
	count := "One configuration"
	if len(exts) != 1 {
		count = fmt.Sprintf("%d configurations, one per extension", len(exts))
	}
	under := ""
	if keyPrefix != "" {
		under = ", under " + keyPrefix
	}
	return filters, true, fmt.Sprintf("%s%s. S3 suffix filters are case-sensitive: a file named .%s is not sent.", count, under, strings.ToUpper(exts[0]))
}

// BucketNotification is the bucket's notification configuration: every object created under the
// filters goes to the queue (queueARN) or, when queueARN is "", the topic.
func BucketNotification(queueARN, topicARN, keyPrefix, pattern string) Obj {
	filters, _, _ := NotificationFilters(keyPrefix, pattern)
	configurations := make([]any, 0, len(filters))
	for _, f := range filters {
		var rules []any
		if f.Prefix != "" {
			rules = append(rules, Obj{{"Name", "prefix"}, {"Value", f.Prefix}})
		}
		if f.Suffix != "" {
			rules = append(rules, Obj{{"Name", "suffix"}, {"Value", f.Suffix}})
		}
		c := Obj{{"Id", f.ID()}}
		if queueARN != "" {
			c = append(c, KV{"QueueArn", queueARN})
		} else {
			c = append(c, KV{"TopicArn", topicARN})
		}
		c = append(c, KV{"Events", strs("s3:ObjectCreated:*")})
		if len(rules) > 0 {
			c = append(c, KV{"Filter", Obj{{"Key", Obj{{"FilterRules", rules}}}}})
		}
		configurations = append(configurations, c)
	}
	if queueARN != "" {
		return Obj{{"QueueConfigurations", configurations}}
	}
	return Obj{{"TopicConfigurations", configurations}}
}

// ---- Commands ------------------------------------------------------------------------------

// ShellQuote quotes for a POSIX shell.
func ShellQuote(value string) string {
	if shellSafeRe.MatchString(value) {
		return value
	}
	return "'" + strings.ReplaceAll(value, "'", `'\''`) + "'"
}

func regionFlag(region string) string {
	if region != "" {
		return " --region " + region
	}
	return ""
}

// PutBucketNotificationCommand saves the notification configuration on the bucket.
func PutBucketNotificationCommand(bucket string, notification Obj, region string) string {
	return fmt.Sprintf("aws s3api put-bucket-notification-configuration --bucket %s%s \\\n  --notification-configuration %s",
		ShellQuote(bucket), regionFlag(region), ShellQuote(Stringify(notification)))
}

// SetQueuePolicyCommand sets the queue's access policy.
func SetQueuePolicyCommand(queueURL string, p Obj) string {
	url := strings.TrimSpace(queueURL)
	attributes := Obj{{"Policy", Stringify(p)}}
	return fmt.Sprintf("aws sqs set-queue-attributes --queue-url %s%s \\\n  --attributes %s",
		ShellQuote(url), regionFlag(QueueRegion(url)), ShellQuote(Stringify(attributes)))
}

// SetTopicPolicyCommand sets the topic's access policy.
func SetTopicPolicyCommand(topicARN string, p Obj) string {
	arn := strings.TrimSpace(topicARN)
	return fmt.Sprintf("aws sns set-topic-attributes --topic-arn %s%s \\\n  --attribute-name Policy --attribute-value %s",
		ShellQuote(arn), regionFlag(TopicRegion(arn)), ShellQuote(Stringify(p)))
}

// SnsSubscribeCommand subscribes the queue (`sqs`, with its ARN) or the hook URL (`https`) to the topic.
func SnsSubscribeCommand(topicARN, protocol, endpoint string) string {
	arn := strings.TrimSpace(topicARN)
	return fmt.Sprintf("aws sns subscribe --topic-arn %s%s \\\n  --protocol %s --notification-endpoint %s",
		ShellQuote(arn), regionFlag(TopicRegion(arn)), protocol, ShellQuote(strings.TrimSpace(endpoint)))
}

// OutputLoopRisk reports whether outputs delivered to the same bucket could land under the source
// prefix and be taken again: the destination template's fixed start (up to the first variable) must
// not sit inside the source prefix.
func OutputLoopRisk(sourcePrefix, destinationTemplate string) bool {
	source := NormalizeRoot(sourcePrefix)
	fixed := strings.SplitN(strings.TrimLeft(strings.TrimSpace(destinationTemplate), "/"), "{", 2)[0]
	// A fixed start shorter than the source prefix (or none, as with `{dir}/…`) may still render inside it.
	return source == "" || strings.HasPrefix(fixed, source) || strings.HasPrefix(source, fixed)
}

func contains(list []string, value string) bool {
	for _, v := range list {
		if v == value {
			return true
		}
	}
	return false
}
