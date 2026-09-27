package setup

import (
	"errors"
	"fmt"
	"strings"
)

// Methods: how new files reach Transcdr. Each creates the automation trigger of the same name,
// except webhook, whose trigger is `hook`.
const (
	MethodWatch   = "watch"
	MethodQueue   = "queue"
	MethodWebhook = "webhook"
)

// Fan-out: S3 sends to the queue directly, or through an SNS topic the queue subscribes to.
const (
	FanoutDirect = "direct"
	FanoutSNS    = "sns"
)

// DefaultPattern is the pattern an automation gets when none is given.
const DefaultPattern = "**/*.{mp4,mov,mkv,webm,m4v,avi,ts,mts,m2ts,mxf}"

// Input is a bucket automation to set up.
type Input struct {
	Method string
	// Fanout applies to the queue method; "" means direct.
	Fanout string
	Bucket string
	// Region is the bucket's, for the CLI commands.
	Region string
	// Root is the storage connection's folder.
	Root string
	// Prefix and Pattern are the automation's; Pattern defaults to DefaultPattern.
	Prefix  string
	Pattern string
	// QueueURL and QueueARN name the trigger queue. The ARN is read from an AWS queue URL; give it
	// for any other URL.
	QueueURL string
	QueueARN string
	// TopicARN is the SNS topic: the queue method's fan-out, or the webhook method's sender.
	TopicARN        string
	OutputsToBucket bool
	DeleteSource    bool
}

// Filter is one notification configuration's filter.
type Filter struct {
	ID     string
	Prefix string
	// Suffix is "" for none.
	Suffix string
}

// Result is everything generated for an Input. The policy and notification fields are compact JSON,
// "" when they do not apply to the method.
type Result struct {
	Roles              []string
	KeyPrefix          string
	Pattern            string
	BucketPolicy       string
	ConsumerPolicy     string
	IAMPolicy          string
	QueuePolicy        string
	TopicPolicy        string
	BucketNotification string
	Filters            []Filter
	SuffixFilters      []string
	BySuffix           bool
	FiltersNote        string
	QueueARN           string
	Commands           []string
}

// Generate builds the policies, notification and commands for a bucket automation.
func Generate(in Input) (Result, error) {
	var r Result
	switch in.Method {
	case MethodWatch, MethodQueue, MethodWebhook:
	default:
		return r, fmt.Errorf("method must be watch, queue or webhook, not %q", in.Method)
	}
	fanout := in.Fanout
	if fanout == "" {
		fanout = FanoutDirect
	}
	if fanout != FanoutDirect && fanout != FanoutSNS {
		return r, fmt.Errorf("fanout must be direct or sns, not %q", in.Fanout)
	}
	if strings.TrimSpace(in.Bucket) == "" {
		return r, errors.New("bucket is required")
	}
	topicARN := strings.TrimSpace(in.TopicARN)
	if topicARN != "" && !IsTopicArn(topicARN) {
		return r, fmt.Errorf("topic_arn must be an SNS topic ARN (arn:aws:sns:<region>:<account id>:<topic>), not %q", in.TopicARN)
	}
	if in.Method == MethodQueue && fanout == FanoutSNS && topicARN == "" {
		return r, errors.New("fan-out through SNS needs topic_arn")
	}

	pattern := strings.TrimSpace(in.Pattern)
	if pattern == "" {
		pattern = DefaultPattern
	}
	r.Pattern = pattern
	r.Roles = RolesFor(in.Method, in.OutputsToBucket)
	r.KeyPrefix = NotificationPrefix(in.Root, in.Prefix)
	bucketPolicy := S3PolicyTemplate(in.Bucket, in.Root, r.Roles, in.DeleteSource)
	r.BucketPolicy = Stringify(bucketPolicy)
	r.IAMPolicy = r.BucketPolicy

	filters, bySuffix, note := NotificationFilters(r.KeyPrefix, pattern)
	r.BySuffix, r.FiltersNote = bySuffix, note
	r.SuffixFilters = []string{}
	for _, f := range filters {
		r.Filters = append(r.Filters, Filter{ID: f.ID(), Prefix: f.Prefix, Suffix: f.Suffix})
		if f.Suffix != "" {
			r.SuffixFilters = append(r.SuffixFilters, f.Suffix)
		}
	}

	switch in.Method {
	case MethodQueue:
		queueURL := strings.TrimSpace(in.QueueURL)
		arn := strings.TrimSpace(in.QueueARN)
		if arn == "" {
			if queueURL == "" {
				return r, errors.New("the queue method needs queue_url or queue_arn")
			}
			if arn = QueueARN(queueURL); arn == "" {
				return r, errors.New("queue_arn is required when queue_url is not an AWS queue URL (https://sqs.<region>.amazonaws.com/<account id>/<queue>)")
			}
		}
		r.QueueARN = arn
		queue := SQSQueueSetupARN(arn, in.Bucket, "")
		fanned := SQSQueueSetupARN(arn, "", topicARN)
		r.ConsumerPolicy = Stringify(queue.IAMPolicy)
		r.IAMPolicy = Stringify(MergePolicies(bucketPolicy, queue.IAMPolicy))
		var queuePolicy, notification Obj
		if fanout == FanoutSNS {
			queuePolicy = fanned.QueuePolicyForSNS
			notification = BucketNotification("", topicARN, r.KeyPrefix, pattern)
			topicPolicy := TopicPolicyForS3(topicARN, in.Bucket)
			r.TopicPolicy = Stringify(topicPolicy)
			r.Commands = append(r.Commands, PutBucketNotificationCommand(in.Bucket, notification, in.Region))
			if queueURL != "" {
				r.Commands = append(r.Commands, SetQueuePolicyCommand(queueURL, queuePolicy))
			}
			r.Commands = append(r.Commands, SetTopicPolicyCommand(topicARN, topicPolicy), SnsSubscribeCommand(topicARN, "sqs", arn))
		} else {
			queuePolicy = queue.QueuePolicyForS3
			notification = BucketNotification(arn, "", r.KeyPrefix, pattern)
			if queueURL != "" {
				r.Commands = append(r.Commands, SetQueuePolicyCommand(queueURL, queuePolicy))
			}
			r.Commands = append(r.Commands, PutBucketNotificationCommand(in.Bucket, notification, in.Region))
		}
		r.QueuePolicy = Stringify(queuePolicy)
		r.BucketNotification = Stringify(notification)
	case MethodWebhook:
		if topicARN != "" {
			notification := BucketNotification("", topicARN, r.KeyPrefix, pattern)
			topicPolicy := TopicPolicyForS3(topicARN, in.Bucket)
			r.TopicPolicy = Stringify(topicPolicy)
			r.BucketNotification = Stringify(notification)
			r.Commands = append(r.Commands, SetTopicPolicyCommand(topicARN, topicPolicy), PutBucketNotificationCommand(in.Bucket, notification, in.Region))
		}
	}
	if r.Commands == nil {
		r.Commands = []string{}
	}
	return r, nil
}
