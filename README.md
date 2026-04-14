# ⏰ Cron Impulse

A time-based trigger for BubuStack that submits durable `StoryTrigger` requests according to cron schedules.

## 🌟 Highlights

The Cron Impulse enables scheduled automation workflows without external dependencies. It supports:

- **Multiple schedules** with different inputs per schedule
- **Standard cron expressions** (5-field format)
- **Timezone support** (IANA format)
- **Jitter** to spread load across instances
- **Concurrency policies** to control overlapping runs
- **Health endpoints** for Kubernetes probes

## 🏗️ Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                      Cron Impulse                           │
│                                                             │
│  ┌─────────────┐    ┌──────────────┐    ┌──────────────┐   │
│  │   Cron      │───▶│ StoryTrigger │───▶│  StoryRun    │   │
│  │  Scheduler  │    │   Handler    │    │  Dispatcher  │   │
│  └─────────────┘    └──────────────┘    └──────────────┘   │
│         │                                      │            │
│         │           ┌──────────────┐           │            │
│         └──────────▶│  Health API  │           │            │
│                     │  :8080       │           │            │
│                     └──────────────┘           │            │
└────────────────────────────────────────────────│────────────┘
                                                 │
                                                 ▼
                                          ┌──────────────┐
                                          │  Kubernetes  │
                                          │    API       │
                                          └──────────────┘
```

## 🚀 Quick Start

### 1. Install the ImpulseTemplate

```bash
kubectl apply -f Impulse.yaml
```

### 2. Create an Impulse instance

```yaml
apiVersion: bubustack.io/v1alpha1
kind: Impulse
metadata:
  name: daily-tasks
  namespace: my-namespace
spec:
  templateRef:
    name: cron
  
  # Target story is specified via storyRef (required by Impulse CRD)
  storyRef:
    name: daily-report
  
  # Config defines the cron-specific settings
  with:
    timezone: "America/New_York"
    schedules:
      - name: morning-report
        description: Generate daily report at 8 AM
        cron: "0 8 * * *"
        inputs:
          reportType: daily
          recipients:
            - team@example.com

      - name: hourly-sync
        description: Sync data every hour
        cron: "@hourly"
        inputs:
          source: production
```

## ⚙️ Configuration (`Impulse.spec.with`)

The target Story is specified via the standard `Impulse.spec.storyRef` field, not in the config.
This follows the BubuStack CRD design where all Impulses must define their target story in `spec.storyRef`.

### Config Schema (`spec.with`)

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `schedules` | `[]Schedule` | **Yes** | - | List of cron schedules |
| `timezone` | `string` | No | `UTC` | IANA timezone |
| `runOnStartup` | `bool` | No | `false` | Trigger all on startup |
| `concurrencyPolicy` | `string` | No | `allow` | `allow`, `forbid`, or `replace` |

### Schedule Schema

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `name` | `string` | **Yes** | - | Schedule identifier |
| `cron` | `string` | **Yes** | - | Cron expression |
| `description` | `string` | No | - | Human-readable description |
| `inputs` | `object` | No | `{}` | Story inputs for this schedule |
| `metadata` | `map[string]string` | No | `{}` | StoryRun annotations |
| `enabled` | `bool` | No | `true` | Enable/disable schedule |
| `jitter` | `duration` | No | `0` | Random delay before trigger |

### Cron Expression Format

Standard 5-field cron format:

```
┌───────────── minute (0-59)
│ ┌───────────── hour (0-23)
│ │ ┌───────────── day of month (1-31)
│ │ │ ┌───────────── month (1-12)
│ │ │ │ ┌───────────── day of week (0-6, Sun=0)
│ │ │ │ │
* * * * *
```

**Supported descriptors:**
- `@yearly` / `@annually` - Once a year (Jan 1, midnight)
- `@monthly` - Once a month (1st, midnight)
- `@weekly` - Once a week (Sunday, midnight)
- `@daily` / `@midnight` - Once a day (midnight)
- `@hourly` - Once an hour (top of hour)
- `@every <duration>` - Every duration (e.g., `@every 5m`)

### Examples

```yaml
# Every 5 minutes
cron: "*/5 * * * *"

# 8 AM every weekday
cron: "0 8 * * 1-5"

# First day of every month at noon
cron: "0 12 1 * *"

# Every hour
cron: "@hourly"

# Every 30 seconds (using @every)
cron: "@every 30s"
```

## 🔄 Concurrency Policies

| Policy | Behavior |
|--------|----------|
| `allow` | Allow multiple runs of the same schedule concurrently (default) |
| `forbid` | Skip new run if previous StoryRun session is still active |
| `replace` | Stop previous StoryRun session, then start new one |

## 🩺 Health Endpoints

| Endpoint | Description |
|----------|-------------|
| `GET /health` | Liveness probe - always returns 200 |
| `GET /ready` | Readiness probe - 200 if schedules registered |
| `GET /schedules` | Lists all registered schedules with next run time |

## 📥 Story Inputs

Each resolved StoryRun receives inputs with an additional `_cron` metadata block:

```json
{
  "reportType": "daily",
  "recipients": ["team@example.com"],
  "_cron": {
    "schedule": "morning-report",
    "description": "Generate daily report at 8 AM",
    "triggeredAt": "2026-01-19T08:00:00-05:00",
    "runID": "a1b2c3d4e5f6g7h8"
  }
}
```

## 🧪 Local Development

### Build locally

```bash
make build
```

### Run tests

```bash
make test
```

### Build Docker image

```bash
make docker-build
```

### Push to registry

```bash
make docker-push
```


## 🤝 Community & Support

- [Contributing](./CONTRIBUTING.md)
- [Support](./SUPPORT.md)
- [Security Policy](./SECURITY.md)
- [Code of Conduct](./CODE_OF_CONDUCT.md)
- [Discord](https://discord.gg/dysrB7D8H6)

## 📄 License

Copyright 2025 BubuStack.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
