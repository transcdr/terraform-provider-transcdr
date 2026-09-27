// Regenerates golden.json from the dashboard's own generators, so the Go port is checked against
// the real TypeScript: the cases in cases.json run through transcdr-frontend's src/lib/automate.ts
// and wizard.ts (bundled with the frontend's esbuild).
//
//   node internal/setup/testdata/gen_golden.mjs [path/to/transcdr-frontend]
//
// The frontend defaults to a sibling checkout of this repository (TRANSCDR_FRONTEND overrides it).

import { createRequire } from 'node:module';
import { mkdtempSync, readFileSync, rmSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { dirname, join, resolve } from 'node:path';
import { fileURLToPath, pathToFileURL } from 'node:url';

const here = dirname(fileURLToPath(import.meta.url));
const frontend = resolve(process.argv[2] ?? process.env.TRANSCDR_FRONTEND ?? join(here, '../../../../transcdr-frontend'));
const esbuild = createRequire(join(frontend, 'package.json'))('esbuild');

const dir = mkdtempSync(join(tmpdir(), 'transcdr-golden-'));
const entry = join(dir, 'entry.ts');
const lib = join(frontend, 'src/lib').replace(/\\/g, '/');
writeFileSync(entry, `export * from '${lib}/automate.ts';\nexport { normalizeRoot, queueRegion, queueArn, s3PolicyTemplate, sqsQueueSetup } from '${lib}/wizard.ts';\n`);
const out = join(dir, 'lib.mjs');
await esbuild.build({ entryPoints: [entry], bundle: true, format: 'esm', platform: 'node', outfile: out, logLevel: 'error' });
const L = await import(pathToFileURL(out).href);
rmSync(dir, { recursive: true, force: true });

const cases = JSON.parse(readFileSync(join(here, 'cases.json'), 'utf8'));
const s = JSON.stringify;
const str = (v) => (v === null || v === undefined ? '' : typeof v === 'string' ? v : s(v));
const target = (queueArn, topicArn) => (queueArn ? { queueArn } : { topicArn });

const fns = {
  normalizeRoot: (root) => L.normalizeRoot(root),
  queueRegion: (url) => L.queueRegion(url),
  queueArn: (url) => L.queueArn(url),
  topicRegion: (arn) => L.topicRegion(arn),
  arnAccount: (arn) => L.arnAccount(arn),
  isTopicArn: (v) => L.isTopicArn(v),
  s3PolicyTemplate: (bucket, root, roles, deleteSource) => L.s3PolicyTemplate(bucket, root, roles ?? undefined, { deleteSource }),
  sqsQueueSetup: (url, region, bucket, topicArn) => {
    const setup = L.sqsQueueSetup(url, region, { bucket, topicArn });
    return { iam_policy: s(setup.iam_policy), queue_policy_for_s3: s(setup.queue_policy_for_s3), queue_policy_for_sns: s(setup.queue_policy_for_sns) };
  },
  consumerPolicy: (url) => L.consumerPolicy(url),
  queueAccessPolicy: (url, bucket, topicArn) => L.queueAccessPolicy(url, topicArn ? { topicArn } : { bucket }),
  topicPolicyForS3: (topicArn, bucket) => L.topicPolicyForS3(topicArn, bucket),
  notificationPrefix: (root, prefix) => L.notificationPrefix(root, prefix),
  patternExtensions: (pattern) => L.patternExtensions(pattern),
  notificationFilters: (keyPrefix, pattern) => {
    const f = L.notificationFilters(keyPrefix, pattern);
    return { filters: f.filters.map((x) => ({ prefix: x.prefix, suffix: x.suffix ?? '' })), bySuffix: f.bySuffix, note: f.note };
  },
  bucketNotification: (queueArn, topicArn, keyPrefix, pattern) => L.bucketNotification(target(queueArn, topicArn), keyPrefix, pattern),
  shellQuote: (v) => L.shellQuote(v),
  putBucketNotificationCommand: (bucket, queueArn, topicArn, keyPrefix, pattern, region) =>
    L.putBucketNotificationCommand(bucket, L.bucketNotification(target(queueArn, topicArn), keyPrefix, pattern), region),
  setQueuePolicyCommand: (url, bucket) => L.setQueuePolicyCommand(url, L.queueAccessPolicy(url, { bucket })),
  setTopicPolicyCommand: (topicArn, bucket) => L.setTopicPolicyCommand(topicArn, L.topicPolicyForS3(topicArn, bucket)),
  snsSubscribeCommand: (topicArn, protocol, endpoint) => L.snsSubscribeCommand(topicArn, protocol, endpoint),
  outputLoopRisk: (source, template) => L.outputLoopRisk(source, template),
};

// The data source's composition (internal/setup/generate.go), from the TypeScript functions.
function generate(c) {
  const pattern = (c.pattern ?? '').trim() || '**/*.{mp4,mov,mkv,webm,m4v,avi,ts,mts,m2ts,mxf}';
  const roles = L.rolesFor(c.method, Boolean(c.outputs_to_bucket));
  const keyPrefix = L.notificationPrefix(c.root ?? '', c.prefix ?? '');
  const bucketPolicy = L.bucketPolicy(c.bucket, c.root ?? '', roles, { deleteSource: Boolean(c.delete_source) });
  const f = L.notificationFilters(keyPrefix, pattern);
  const r = {
    roles,
    key_prefix: keyPrefix,
    pattern,
    bucket_policy: s(bucketPolicy),
    consumer_policy: '',
    iam_policy: s(bucketPolicy),
    queue_policy: '',
    topic_policy: '',
    bucket_notification: '',
    filters: f.filters.map((x) => ({ id: x.suffix ? `transcdr-${x.suffix.slice(1)}` : 'transcdr', prefix: x.prefix, suffix: x.suffix ?? '' })),
    suffix_filters: f.filters.filter((x) => x.suffix).map((x) => x.suffix),
    by_suffix: f.bySuffix,
    filters_note: f.note,
    queue_arn: '',
    commands: [],
  };
  const topicArn = (c.topic_arn ?? '').trim();
  if (c.method === 'queue') {
    const url = c.queue_url.trim();
    const arn = L.queueArn(url);
    const consumer = L.consumerPolicy(url);
    r.queue_arn = arn;
    r.consumer_policy = s(consumer);
    r.iam_policy = s(L.mergePolicies(bucketPolicy, consumer));
    if (c.fanout === 'sns') {
      const queuePolicy = L.queueAccessPolicy(url, { topicArn });
      const notification = L.bucketNotification({ topicArn }, keyPrefix, pattern);
      const topicPolicy = L.topicPolicyForS3(topicArn, c.bucket);
      r.queue_policy = s(queuePolicy);
      r.bucket_notification = s(notification);
      r.topic_policy = s(topicPolicy);
      r.commands = [
        L.putBucketNotificationCommand(c.bucket, notification, c.region),
        L.setQueuePolicyCommand(url, queuePolicy),
        L.setTopicPolicyCommand(topicArn, topicPolicy),
        L.snsSubscribeCommand(topicArn, 'sqs', arn),
      ];
    } else {
      const queuePolicy = L.queueAccessPolicy(url, { bucket: c.bucket });
      const notification = L.bucketNotification({ queueArn: arn }, keyPrefix, pattern);
      r.queue_policy = s(queuePolicy);
      r.bucket_notification = s(notification);
      r.commands = [L.setQueuePolicyCommand(url, queuePolicy), L.putBucketNotificationCommand(c.bucket, notification, c.region)];
    }
  } else if (c.method === 'webhook' && topicArn) {
    const notification = L.bucketNotification({ topicArn }, keyPrefix, pattern);
    const topicPolicy = L.topicPolicyForS3(topicArn, c.bucket);
    r.topic_policy = s(topicPolicy);
    r.bucket_notification = s(notification);
    r.commands = [L.setTopicPolicyCommand(topicArn, topicPolicy), L.putBucketNotificationCommand(c.bucket, notification, c.region)];
  }
  return r;
}

const golden = {
  functions: cases.functions.map((c) => {
    const result = fns[c.fn](...c.args);
    return { fn: c.fn, args: c.args, out: typeof result === 'object' && result !== null && !Array.isArray(result) && c.fn === 'sqsQueueSetup' ? result : str(result) };
  }),
  generate: cases.generate.map((c) => ({ name: c.name, out: generate(c) })),
};
writeFileSync(join(here, 'golden.json'), `${JSON.stringify(golden, null, 2)}\n`);
console.log(`golden.json: ${golden.functions.length} function cases, ${golden.generate.length} generate cases`);
