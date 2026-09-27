package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/transcdr/terraform-provider-transcdr/internal/setup"
)

func newBucketAutomationSetupDataSource() datasource.DataSource {
	return &bucketAutomationSetupDataSource{}
}

// bucketAutomationSetupDataSource generates, locally and without an API call, what AWS needs for a
// bucket automation: the same policies and notification as the dashboard's "Automate a bucket".
type bucketAutomationSetupDataSource struct{}

type bucketAutomationSetupModel struct {
	Method             types.String `tfsdk:"method"`
	Fanout             types.String `tfsdk:"fanout"`
	Bucket             types.String `tfsdk:"bucket"`
	Region             types.String `tfsdk:"region"`
	Root               types.String `tfsdk:"root"`
	Prefix             types.String `tfsdk:"prefix"`
	Pattern            types.String `tfsdk:"pattern"`
	QueueURL           types.String `tfsdk:"queue_url"`
	QueueARN           types.String `tfsdk:"queue_arn"`
	TopicARN           types.String `tfsdk:"topic_arn"`
	OutputsToBucket    types.Bool   `tfsdk:"outputs_to_bucket"`
	DeleteSource       types.Bool   `tfsdk:"delete_source"`
	Trigger            types.String `tfsdk:"trigger"`
	Roles              types.List   `tfsdk:"roles"`
	KeyPrefix          types.String `tfsdk:"key_prefix"`
	BucketPolicy       types.String `tfsdk:"bucket_policy"`
	ConsumerPolicy     types.String `tfsdk:"consumer_policy"`
	IAMPolicy          types.String `tfsdk:"iam_policy"`
	QueuePolicy        types.String `tfsdk:"queue_policy"`
	TopicPolicy        types.String `tfsdk:"topic_policy"`
	BucketNotification types.String `tfsdk:"bucket_notification"`
	NotificationFilter types.List   `tfsdk:"notification_filters"`
	SuffixFilters      types.List   `tfsdk:"suffix_filters"`
	BySuffix           types.Bool   `tfsdk:"by_suffix"`
	FiltersNote        types.String `tfsdk:"filters_note"`
	ResolvedQueueARN   types.String `tfsdk:"resolved_queue_arn"`
	Commands           types.List   `tfsdk:"commands"`
}

var filterType = map[string]attr.Type{
	"id":            types.StringType,
	"filter_prefix": types.StringType,
	"filter_suffix": types.StringType,
	"events":        types.ListType{ElemType: types.StringType},
}

func (d *bucketAutomationSetupDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_bucket_automation_setup"
}

func (d *bucketAutomationSetupDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	in := func(desc string) schema.StringAttribute {
		return schema.StringAttribute{Optional: true, MarkdownDescription: desc}
	}
	out := func(desc string) schema.StringAttribute {
		return schema.StringAttribute{Computed: true, MarkdownDescription: desc}
	}
	resp.Schema = schema.Schema{
		MarkdownDescription: "Generates, locally and without calling the API, what AWS needs so new files in a bucket reach a Transcdr automation: " +
			"the IAM policy for Transcdr's keys, the queue and topic access policies, and the bucket notification with the automation's pattern turned into S3 filters. " +
			"The output is identical to the dashboard's \"Automate a bucket\" setup.\n\n" +
			"The policies are JSON strings for `aws_iam_policy`, `aws_iam_user_policy`, `aws_sqs_queue_policy` and `aws_sns_topic_policy`; " +
			"`notification_filters` feeds `dynamic \"queue\"` or `dynamic \"topic\"` blocks of `aws_s3_bucket_notification`.\n\n" +
			"S3 filters take one prefix and one suffix per configuration: a pattern ending in `*.ext` becomes a suffix filter, `*.{a,b,c}` one configuration per extension, " +
			"and anything else the prefix only (the automation's pattern skips the rest). Suffix filters are case-sensitive.",
		Attributes: map[string]schema.Attribute{
			"method": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "`watch` (Transcdr lists the bucket), `queue` (S3 notifies an SQS queue Transcdr consumes) or `webhook` (S3 notifies an SNS topic with an HTTPS subscription to the automation's `hook_url`).",
				Validators:          []validator.String{stringvalidator.OneOf(setup.MethodWatch, setup.MethodQueue, setup.MethodWebhook)},
			},
			"fanout": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "`queue` only: `direct` (S3 → queue, the default) or `sns` (S3 → SNS topic → queue, when other consumers need the same events; needs `topic_arn`).",
				Validators:          []validator.String{stringvalidator.OneOf(setup.FanoutDirect, setup.FanoutSNS)},
			},
			"bucket":    schema.StringAttribute{Required: true, MarkdownDescription: "The bucket name."},
			"region":    in("The bucket's region, for the `commands`."),
			"root":      in("The storage connection's `root` folder, if any: the policies are limited to it, and notification keys start with it."),
			"prefix":    in("The automation's `source.prefix`, relative to `root`."),
			"pattern":   in("The automation's `source.pattern`. Defaults to the API's default, `" + setup.DefaultPattern + "`."),
			"queue_url": in("`queue`: the queue URL (`aws_sqs_queue.x.url`). Its ARN is read from an AWS URL; give `queue_arn` too for any other."),
			"queue_arn": in("`queue`: the queue ARN (`aws_sqs_queue.x.arn`). Wins over the one read from `queue_url`."),
			"topic_arn": in("The SNS topic ARN: `queue` with `fanout = \"sns\"`, or `webhook` from AWS."),
			"outputs_to_bucket": schema.BoolAttribute{
				Optional:            true,
				MarkdownDescription: "Whether outputs are delivered back to this bucket (the automation's destination is the same connection): adds write access.",
			},
			"delete_source": schema.BoolAttribute{
				Optional:            true,
				MarkdownDescription: "Whether the automation deletes the source once its job completes (`after_success = \"delete\"`): adds `s3:DeleteObject`.",
			},
			"trigger":         out("The automation `trigger` for this method: `watch`, `queue` or `hook`."),
			"roles":           schema.ListAttribute{Computed: true, ElementType: types.StringType, MarkdownDescription: "The storage connection roles the policy covers: `source`, `watch_folder`, `destination`."},
			"key_prefix":      out("The key prefix bucket events carry: `root` then `prefix`."),
			"bucket_policy":   out("The IAM policy for the bucket alone, scoped to the bucket and `root`."),
			"consumer_policy": out("`queue`: the IAM policy that lets Transcdr receive, delete and change visibility of the queue's messages, and read its attributes. Null otherwise."),
			"iam_policy":      out("Everything Transcdr's keys need in one document: `bucket_policy`, plus `consumer_policy` for `queue` (use it when the storage and sqs connections share keys)."),
			"queue_policy":    out("`queue`: the queue access policy that lets S3 (direct) or the topic (fan-out) send to the queue. For `aws_sqs_queue_policy`."),
			"topic_policy":    out("`queue` fan-out, or `webhook` with a `topic_arn`: the topic policy that lets S3 publish the bucket's events. For `aws_sns_topic_policy`."),
			"bucket_notification": out("`queue`, or `webhook` with a `topic_arn`: the bucket notification configuration (`QueueConfigurations` or `TopicConfigurations`), as for " +
				"`aws s3api put-bucket-notification-configuration`. In Terraform use `notification_filters`."),
			"notification_filters": schema.ListNestedAttribute{
				Computed:            true,
				MarkdownDescription: "One entry per notification configuration: `id`, `filter_prefix` and `filter_suffix` (null when none) and `events`.",
				NestedObject: schema.NestedAttributeObject{Attributes: map[string]schema.Attribute{
					"id":            schema.StringAttribute{Computed: true},
					"filter_prefix": schema.StringAttribute{Computed: true},
					"filter_suffix": schema.StringAttribute{Computed: true},
					"events":        schema.ListAttribute{Computed: true, ElementType: types.StringType},
				}},
			},
			"suffix_filters":     schema.ListAttribute{Computed: true, ElementType: types.StringType, MarkdownDescription: "The suffix filters, e.g. `[\".mp4\", \".mov\"]`; empty when the pattern has no simple suffix."},
			"by_suffix":          schema.BoolAttribute{Computed: true, MarkdownDescription: "False when the pattern has no simple suffix, so the notification filters on the prefix only."},
			"filters_note":       out("What to know about the filters, as the dashboard says it."),
			"resolved_queue_arn": out("`queue`: the queue ARN the policies use."),
			"commands":           schema.ListAttribute{Computed: true, ElementType: types.StringType, MarkdownDescription: "The equivalent AWS CLI commands, for reference."},
		},
	}
}

func (d *bucketAutomationSetupDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var m bucketAutomationSetupModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	in := setup.Input{
		Method:          m.Method.ValueString(),
		Fanout:          m.Fanout.ValueString(),
		Bucket:          m.Bucket.ValueString(),
		Region:          m.Region.ValueString(),
		Root:            m.Root.ValueString(),
		Prefix:          m.Prefix.ValueString(),
		Pattern:         m.Pattern.ValueString(),
		QueueURL:        m.QueueURL.ValueString(),
		QueueARN:        m.QueueARN.ValueString(),
		TopicARN:        m.TopicARN.ValueString(),
		OutputsToBucket: m.OutputsToBucket.ValueBool(),
		DeleteSource:    m.DeleteSource.ValueBool(),
	}
	r, err := setup.Generate(in)
	if err != nil {
		resp.Diagnostics.AddError("Invalid bucket automation setup", err.Error())
		return
	}
	trigger := in.Method
	if trigger == setup.MethodWebhook {
		trigger = "hook"
	}
	m.Trigger = types.StringValue(trigger)
	m.Roles = stringList(r.Roles)
	m.KeyPrefix = types.StringValue(r.KeyPrefix)
	m.BucketPolicy = orNull(r.BucketPolicy)
	m.ConsumerPolicy = orNull(r.ConsumerPolicy)
	m.IAMPolicy = orNull(r.IAMPolicy)
	m.QueuePolicy = orNull(r.QueuePolicy)
	m.TopicPolicy = orNull(r.TopicPolicy)
	m.BucketNotification = orNull(r.BucketNotification)
	m.SuffixFilters = stringList(r.SuffixFilters)
	m.BySuffix = types.BoolValue(r.BySuffix)
	m.FiltersNote = types.StringValue(r.FiltersNote)
	m.ResolvedQueueARN = orNull(r.QueueARN)
	m.Commands = stringList(r.Commands)

	filters := make([]attr.Value, 0, len(r.Filters))
	for _, f := range r.Filters {
		v, diags := types.ObjectValue(filterType, map[string]attr.Value{
			"id":            types.StringValue(f.ID),
			"filter_prefix": orNull(f.Prefix),
			"filter_suffix": orNull(f.Suffix),
			"events":        stringList([]string{"s3:ObjectCreated:*"}),
		})
		resp.Diagnostics.Append(diags...)
		filters = append(filters, v)
	}
	list, diags := types.ListValue(types.ObjectType{AttrTypes: filterType}, filters)
	resp.Diagnostics.Append(diags...)
	m.NotificationFilter = list
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}

func orNull(s string) types.String {
	if s == "" {
		return types.StringNull()
	}
	return types.StringValue(s)
}
