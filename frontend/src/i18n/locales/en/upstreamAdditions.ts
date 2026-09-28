// Messages added in upstream d681d079, kept separate from custom locale overrides.
export default {
  "keys": {
    "useKeyModal": {
      "deepseek": {
        "description": "Configure Claude Code, Codex, or OpenCode through the current DeepSeek group.",
        "codexDescription": "Configure Codex with API key authentication through the current DeepSeek group.",
        "codexConfigTomlHint": "Download the model catalog below, save both files under the Codex config directory, and restart Codex.",
        "codexNote": "Export SUB2API_API_KEY before starting Codex. The downloaded catalog contains model metadata only, not your API key."
      },
      "minimax": {
        "description": "Configure Claude Code, Codex, or OpenCode through the current MiniMax group.",
        "codexDescription": "Configure Codex with API key authentication through the current MiniMax group.",
        "codexConfigTomlHint": "Download the model catalog below, save both files under the Codex config directory, and restart Codex.",
        "codexNote": "Export SUB2API_API_KEY before starting Codex. The downloaded catalog contains model metadata only, not your API key."
      },
      "composite": {
        "description": "Configure supported clients through the current Composite routing group.",
        "codexDescription": "Configure Codex with API key authentication and the complete model catalog for this Composite group.",
        "codexConfigTomlHint": "Download the model catalog below, save both files under the Codex config directory, and restart Codex.",
        "codexNote": "Export SUB2API_API_KEY before starting Codex. Model requests are routed by the selected catalog slug."
      },
      "routedCodex": {
        "description": "Configure Codex with the complete model catalog for the current routed group.",
        "configTomlHint": "Download the model catalog below, save both files under the Codex config directory, and restart Codex.",
        "note": "Export SUB2API_API_KEY before starting Codex. The downloaded catalog contains model metadata only, not your API key."
      },
      "codexModelCatalog": {
        "title": "Codex model catalog",
        "description": "Fetch with this API key, then save the catalog at the path referenced by config.toml.",
        "fetch": "Fetch catalog",
        "retry": "Retry",
        "download": "Download catalog",
        "modelsCount": "{count} models ready to download",
        "errorDescription": "The catalog could not be fetched with this API key."
      }
    }
  },
  "usage": {
    "requestedReasoningEffort": "Requested reasoning effort",
    "nativeCompactionV2": "Compaction",
    "compactionFilter": "Request Kind",
    "allCompactionTypes": "All Requests",
    "compactionOnly": "Compaction Only",
    "serviceTierUltrafast": "Ultrafast"
  },
  "monitorCommon": {
    "providers": {
      "antigravity": "Antigravity",
      "kimi": "Kimi",
      "zhipu": "Zhipu GLM",
      "deepseek": "DeepSeek",
      "minimax": "MiniMax",
      "opencode_go": "OpenCode Go"
    },
    "checkMode": {
      "probe": "Probe",
      "quota": "Quota",
      "quota_probe": "Probe + Quota"
    },
    "quota": {
      "unavailable": "Quota unavailable",
      "windows": {
        "5h": "5h",
        "7d": "7d",
        "7dSonnet": "7d Sonnet",
        "7dFable": "7d Fable",
        "weekly": "Weekly",
        "daily": "Daily",
        "30d": "30d",
        "total": "Total"
      },
      "labels": {
        "requests": "Requests",
        "tokens": "Tokens",
        "shared": "Shared",
        "pro": "Pro",
        "flash": "Flash"
      }
    }
  },
  "availableChannels": {
    "pricing": {
      "cacheWrite5mPrice": "Cache Write (5m)",
      "cacheWrite1hPrice": "Cache Write (1h)"
    }
  },
  "modelPlaza": {
    "detail": {
      "longContextDisabledNote": "Long-context tier pricing is disabled for this group: requests above the threshold are billed at the base tier; official tiers are for reference only"
    },
    "table": {
      "cacheWriteShort": "W",
      "cacheReadShort": "R",
      "tierHint": "The whole request is billed at the tier matching its total context (input + cache write + cache read)",
      "tierHintMarginal": "Only the portion above the threshold is billed at this tier; output is unaffected",
      "maxReasoningMultiplierBadge": "Max ×{multiplier}",
      "maxReasoningMultiplierHint": "When the forwarded reasoning effort is max, billing and quota usage for the request are multiplied by {multiplier}",
      "reasoningMultiplierBadge": "{effort} ×{multiplier}",
      "reasoningMultiplierHint": "The billing multiplier for reasoning effort {effort} is {multiplier}",
      "marginalBadge": "excess-only tiers",
      "timePricingRowHint": "Requests made within this period ({timezone} time) are billed at the prices in this row",
      "timePricingRowHintWeekdays": "On weekdays (Mon–Fri) only, requests made within this period ({timezone} time) are billed at the prices in this row; weekends use the standard prices",
      "timePricingRowHintPeak": "; prices in this row exclude the peak-hour rate — where this period overlaps the peak hours {window}, the overlapping portion is additionally multiplied by ×{multiplier}",
      "timePricingWeekdays": "Weekdays",
      "timePricingRateHint": "Effective rate {rate} × period multiplier {multiplier}"
    }
  },
  "admin": {
    "backup": {
      "columns": {
        "parts": "Parts"
      },
      "actions": {
        "downloadParts": "Download Parts",
        "downloadPartsHint": "Download every part in order and concatenate the gzip bytes: on Linux/macOS run cat payload.part-* > backup.sql.gz; on Windows run copy /b payload.part-000001+payload.part-000002 backup.sql.gz.",
        "partLabel": "Part {index}",
        "downloadFailed": "Download URL is empty"
      }
    },
    "users": {
      "form": {
        "concurrencyPlaceholder": "0 = unlimited",
        "concurrencyHint": "Max concurrent requests for this user; 0 = unlimited."
      },
      "concurrencyNonNegative": "Concurrency cannot be negative; 0 = unlimited",
      "restrictPublicGroups": "Restrict accessible public groups",
      "restrictPublicGroupsHint": "When on, this user may only use the public groups checked below. When off, every public group stays available.",
      "publicGroupsRestricted": "Public Groups (Restricted)"
    },
    "groups": {
      "usageYesterday": "Yesterday",
      "form": {
        "maxReasoningEffortOverLimit": "Over-limit access control",
        "maxReasoningEffortOverLimitDowngrade": "Automatically downgrade when over limit",
        "maxReasoningEffortOverLimitDeny": "Deny access",
        "maxReasoningEffortOverLimitHint": "Applies after a ceiling is set. Downgrade rewrites values above the ceiling to the ceiling. Deny rejects the request.",
        "reasoningEffortMappingsHint": "Type and model can both be left empty to match every model. One type and model can hold multiple request mappings, for example prefix gpt mapping both high and xhigh to medium. Choose Deny as the forwarded value to reject that request value. Exact matches beat affixes, and longer affixes beat shorter ones.",
        "addReasoningEffortPair": "Add request value",
        "removeReasoningEffortPair": "Remove request value",
        "reasoningEffortMatchType": "Type",
        "reasoningEffortModel": "Model",
        "reasoningEffortMatchExact": "Exact",
        "reasoningEffortMatchPrefix": "Prefix",
        "reasoningEffortMatchSuffix": "Suffix",
        "reasoningEffortMatchTypePlaceholder": "All models",
        "reasoningEffortModelPlaceholder": "Empty = all / gpt / gpt-5.4",
        "reasoningEffortToDeny": "Deny",
        "unsupportedMatchType": "Match type must be exact, prefix, or suffix",
        "duplicateScope": "Type and model combination must be unique"
      },
      "platforms": {
        "kimi": "Kimi",
        "minimax": "MiniMax"
      },
      "videoPricing": {
        "modelOverridesTitle": "Per-model video price overrides",
        "modelOverridesDescription": "Each populated cell overrides the flat resolution price for that model family. Preview and legacy aliases for video-1.5 use the same family; empty cells fall back to the flat resolution price."
      },
      "explicitPricing": {
        "title": "Grok Search & Voice Pricing",
        "description": "Optional per-group prices for web_search (per 1k calls) and Voice realtime / TTS / STT (USD). Leave empty if unused.",
        "searchPricePer1k": "Search price per 1k calls (USD)",
        "pricePlaceholder": "optional"
      },
      "modelPricing": {
        "title": "Per-model group pricing",
        "description": "Overrides channel and built-in prices for matching models. Long-context tiers come from official presets — do not enter custom intervals. Use per-request tiers such as realtime, tts, and stt for audio.",
        "longContext": "Enable long-context tier pricing",
        "longContextHint": "When checked, channel intervals or official preset tiers apply. Otherwise the first tier is used unless the account explicitly enables long-context billing.",
        "add": "Add model price"
      },
      "voicePricing": {
        "title": "Grok Voice Pricing",
        "description": "Optional per-group prices for Voice realtime / TTS / STT (USD). Leave empty to leave unpriced.",
        "audioRealtimePerMin": "Realtime price per minute (USD)",
        "audioTtsPerMillionChars": "TTS price per million chars (USD)",
        "audioSttPerHour": "STT price per hour (USD)",
        "pricePlaceholder": "optional"
      },
      "modelAllowlist": {
        "title": "Model Allowlist",
        "hint": "When enabled, models outside the allowlist are rejected with 404 model_not_found, and model listing endpoints only show allowlisted models. Entries support exact model IDs and trailing * wildcards. Note: Claude Code probes with haiku-family models for titles/summaries and /messages/count_tokens is also allowlist-controlled, so make sure the small models you need are selected too.",
        "loading": "Loading candidate models...",
        "empty": "No candidate models; add custom entries below",
        "selectedSummary": "Selected {selected} / {total}",
        "selectAll": "Select all",
        "invertSelection": "Invert",
        "wildcardTag": "wildcard",
        "customPlaceholder": "Custom entry, e.g. claude-* or gpt-5.5-codex",
        "addCustom": "Add",
        "emptySelectionError": "The model allowlist is enabled; select or add at least one model entry",
        "errors": {
          "empty": "Please enter a model entry",
          "invalidWildcard": "Wildcard * is only allowed at the end of an entry",
          "duplicate": "This entry already exists"
        }
      },
      "codexModelsManifest": {
        "title": "Pinned Accounts for Model Lists",
        "hint": "When enabled, ordinary model lists and Codex Model Manifest are discovered from the pinned accounts first, then merged and filtered using account mappings and the group model list. Rate-limited or overloaded pinned accounts are still used.",
        "enable": "Fetch model lists with specific accounts",
        "enabledHint": "Accounts are limited to OpenAI accounts bound to this group, at most 10.",
        "disabledHint": "Disabled: ordinary lists use local mappings or defaults; Codex uses a local catalog when configured, otherwise scheduler discovery.",
        "accounts": "Pinned accounts",
        "searchPlaceholder": "Search accounts (OpenAI accounts in this group)",
        "searchEmpty": "No matching accounts",
        "fallback": "Fall back to the scheduler when all pinned accounts are unavailable",
        "fallbackHint": "Off: return 503 / the upstream error. On: fall back to the existing scheduler path.",
        "selectAtLeastOne": "Select at least one account after enabling pinned accounts"
      },
      "openaiLive": {
        "enableAnyway": "Enable anyway"
      },
      "openaiFast": {
        "title": "OpenAI Fast mode",
        "force": "Force Fast (priority)",
        "hint": "Forces service_tier=priority on OpenAI requests in this group. The global Fast/Flex policy can still filter or block it. New requests update immediately after saving; existing WebSocket sessions must reconnect.",
        "free": "Free Fast",
        "freeHint": "Fast requests in this group still use the priority tier, but customers are charged the equivalent Standard price."
      }
    },
    "accounts": {
      "platforms": {
        "kimi": "Kimi",
        "minimax": "MiniMax"
      },
      "cnProviders": {
        "accountMode": {
          "title": "Account Type",
          "payg": "Pay-as-you-go",
          "paygDesc": "Consumes account balance, billed per token. Auto-cools down on low balance and recovers after top-up.",
          "coding": "Coding Plan",
          "codingDesc": "Subscription coding package, rate-limited by 5-hour / weekly rolling usage windows."
        },
        "apiProtocol": {
          "title": "API Protocol",
          "adaptive": "Adaptive",
          "adaptiveDesc": "Uses the matching native provider endpoint for each inbound protocol, converting only when unavailable.",
          "endpoints": "Protocol endpoints",
          "responsesFallbackDesc": "Responses requests are converted to Chat Completions because this provider has no native Responses endpoint.",
          "chatCompletions": "Chat Completions",
          "chatCompletionsDesc": "Standard OpenAI-compatible endpoint; requests in other formats are converted.",
          "anthropic": "Anthropic",
          "anthropicDesc": "Native passthrough to the provider’s Anthropic endpoint — ideal for Claude Code.",
          "responses": "Responses",
          "responsesDesc": "Provider’s native Responses endpoint — ideal for Codex."
        },
        "zhipuTeam": {
          "title": "Team Plan Organization / Project ID",
          "organization": "Organization ID (team plan, optional)",
          "organizationPlaceholder": "Organization ID of the team Coding Plan",
          "project": "Project ID (team plan, optional)",
          "projectPlaceholder": "Project ID of the team Coding Plan",
          "hint": "Only required for the team GLM Coding Plan; when set, usage queries go through the team endpoint. Leave empty for personal plans. Click the question mark for how to obtain the IDs.",
          "help": {
            "title": "How to get the Organization / Project ID",
            "step1": "Sign in to the Zhipu open platform (bigmodel.cn) with your team account and open \"Coding Plan → Team → My Plan\".",
            "step2": "Press F12 to open browser DevTools, switch to the Network tab, then reload the page.",
            "step3": "Type /api/biz/v1/organization into the Network filter box and click the matched request (e.g. api_keys).",
            "step4": "In the request URL, the org-… segment is the Organization ID and the proj_… segment is the Project ID (also visible as the bigmodel-organization / bigmodel-project request headers). Fill them into the fields above.",
            "example": "Example: …/organization/org-0610bE2D…/projects/proj_0798F20…/api_keys → org-0610bE2D… goes into \"Organization ID\", proj_0798F20… into \"Project ID\""
          }
        },
        "balance": "Balance --",
        "window5h": "5h",
        "windowWeekly": "7d",
        "windowMonthly": "30d",
        "probe": "Query",
        "probeTooltip": "Query the provider quota endpoint for 5-hour / weekly rolling window usage",
        "balanceProbeTooltip": "Query the provider balance endpoint for the account balance",
        "balanceLow": "Insufficient balance",
        "noBalanceEndpoint": "This platform has no balance query endpoint"
      },
      "accountSchedulingThresholdOverride": "Account Auto-Pause Threshold Override",
      "accountSchedulingThresholdOverrideHint": "Override the platform auto-pause threshold for this account only. Disable to use platform settings.",
      "accountSchedulingThresholdOverrideValue": "Account threshold percent",
      "accountSchedulingThresholdOverrideDisabledHint": "Use 1-100. The account becomes temporarily unschedulable after reaching this usage percent; 100 disables it for this account.",
      "status": {
        "expired": "Expired",
        "tempUnschedulableUntil": "Resumes {time}"
      },
      "tempUnschedulable": {
        "multipleErrorTrigger": "{count} matching errors in {minutes} minutes reached the trigger threshold ({threshold}).",
        "multipleErrorTriggerNoWindow": "{count} matching errors reached the trigger threshold ({threshold}).",
        "multipleErrorCountInWindow": "{count} matching errors occurred within {minutes} minutes.",
        "multipleErrorCount": "{count} matching errors contributed to this block."
      },
      "bulkEdit": {
        "successWithInherited": "Updated {count} account(s). {inherited} selected shadow account(s) still follow their parent account.",
        "partialSuccessWithInherited": "Partially updated: {success} succeeded, {failed} failed. {inherited} selected shadow account(s) still follow their parent account.",
        "longContextShadowHint": "Long-context billing belongs to the parent account. Selected shadow accounts keep following their parent, including when targets come from a filter.",
        "longContextParentRequired": "All selected accounts are shadows. Select the parent account to change long-context billing."
      },
      "upstreamRequestIdHeader": "Upstream ID",
      "upstreamRequestIdHeaderPlaceholder": "Leave empty to record nothing",
      "upstreamRequestIdHeaderHelp": {
        "intro": "Name of the response header in which the direct upstream declares its request ID. The value is recorded in the \"Upstream ID\" column of the usage log; leave empty to record nothing.",
        "examplesTitle": "Common values",
        "sub2apiNote": "Matches the request ID column of its usage log",
        "official": "{platform} official API"
      },
      "openai": {
        "imagesUrlToB64Json": "Image result URL to base64",
        "imagesUrlToB64JsonDesc": "Only applies to non-streaming Images responses of OpenAI API Key accounts. When an upstream image item has a url but no b64_json, the gateway downloads the url and fills b64_json with its base64 content (url is kept) for clients built on the official API; the response is returned unchanged if the download fails.",
        "codexFingerprintMode": "Codex fingerprint convergence",
        "codexFingerprintModeDesc": "When multiple users share the same OAuth account, converge device/session identifiers to account-level stable values to reduce upstream-visible device and session count. Off by default (client identifiers pass through as-is); opt in explicitly when needed. Some accounts reported quota shrinkage after enabling convergence, so choose based on your own measurements.",
        "codexFingerprintOff": "Off (passthrough, default)",
        "codexFingerprintDevice": "Device only",
        "codexFingerprintSession": "Device + Session",
        "codexFingerprintFull": "Full convergence"
      },
      "grok": {
        "testMode": "Test mode",
        "testModeHint": "Text / image / video use the selected model. Web search, TTS, STT and Realtime hit standalone endpoints (not free-form chat tools).",
        "testModeText": "Text (Responses)",
        "testModeImage": "Image (/images/generations)",
        "testModeVideo": "Video (/videos/generations)",
        "testModeSearch": "Web search (/web_search)",
        "testModeTTS": "TTS (/tts)",
        "testModeSTT": "STT (/stt)",
        "testModeRealtime": "Realtime (WS /realtime)",
        "textTestMode": "Mode: Text (Responses)",
        "searchTestMode": "Mode: Web search (/web_search)",
        "ttsTestMode": "Mode: TTS (/tts)",
        "sttTestMode": "Mode: STT (/stt)",
        "realtimeTestMode": "Mode: Realtime (WS /realtime)",
        "searchQueryLabel": "Search query",
        "searchQueryPlaceholder": "Example: xAI Grok",
        "searchQueryDefault": "xAI Grok",
        "searchTestHint": "Standalone web_search probe (same as gateway /v1/web_search). Not a free-form chat with tools.",
        "ttsTextLabel": "TTS text",
        "ttsTextPlaceholder": "Example: Hello from Sub2API connectivity test.",
        "ttsTextDefault": "Hello from Sub2API account connectivity test.",
        "ttsTestHint": "Standalone /v1/tts with language=en; success reports audio byte size.",
        "sttTestHint": "Standalone /v1/stt with a synthetic silent WAV; success means the endpoint is reachable.",
        "realtimeTestHint": "Standalone WebSocket dial to /v1/realtime (model=grok-voice-latest). Handshake success = connectivity OK; may also show the first server event.",
        "sendingSearchRequest": "Sending standalone web_search request...",
        "sendingTTSRequest": "Sending standalone /tts request...",
        "sendingSTTRequest": "Sending standalone /stt request...",
        "sendingRealtimeRequest": "Dialing standalone /realtime WebSocket...",
        "selectedTestMode": "Test mode: {mode}",
        "imageUploadLabel": "Source image (optional, for edits)",
        "videoFirstFrameLabel": "First-frame / reference image (optional)",
        "imageUploadHint": "PNG/JPEG recommended, both sides ≥ 8 px, under ~4 MB for edits. Uploading a source image switches to /images/edits (image-to-image). Leave empty for text-to-image /images/generations.",
        "videoFirstFrameHint": "Optional first-frame / reference image for image-to-video. PNG/JPEG recommended, both sides ≥ 8 px.",
        "audioUploadLabel": "Audio file (optional for STT)",
        "audioUploadHint": "Upload a real audio clip to transcribe. Without a file, a silent WAV is used for connectivity only.",
        "mediaTooLarge": "File is too large (max ~6 MB for admin test uploads).",
        "chooseImageFile": "Choose image",
        "chooseAudioFile": "Choose audio",
        "uploadPreviewAlt": "Upload preview",
        "fileReadFailed": "Failed to read the selected file",
        "noResponseBody": "No response body from server"
      },
      "syncUpstreamModelsMetadataIncomplete": "Model IDs were synced, but no capability metadata could be updated.",
      "syncUpstreamModelsMetadataPartial": "Some model capabilities were updated; remaining models are still incomplete.",
      "grokMediaEligibility": {
        "title": "Media Generation Eligibility",
        "hint": "Controls whether this Grok OAuth account may be selected for image and video generation.",
        "auto": "Automatic detection",
        "enabled": "Force enable",
        "disabled": "Force disable",
        "current": "Current decision:",
        "eligible": "Eligible",
        "ineligible": "Not eligible",
        "loading": "Loading eligibility…",
        "loadFailed": "Unable to load media eligibility",
        "autoHint": "Automatic detection only clears the manual override; it does not trigger a media request.",
        "forceEnableWarning": "Force enable bypasses automatic eligibility checks. Use only for accounts confirmed to support image/video generation.",
        "partialSave": "Other account settings may have been saved, but media eligibility was not updated. Please retry.",
        "reasons": {
          "eligible": "Paid entitlement confirmed",
          "billing_inconclusive": "Billing information inconclusive",
          "billing_forbidden": "Billing endpoint forbidden",
          "billing_free_tier": "Free tier account",
          "billing_unobserved": "Billing not observed yet",
          "override_enabled": "Manually forced enabled",
          "override_disabled": "Manually forced disabled"
        }
      },
      "autoResetCredit": {
        "title": "Automatically use reset credits",
        "hint": "Uses the earliest-expiring available credit only when actual usage reaches a threshold. Off by default; the account remains paused if no credit is available or reset fails.",
        "threshold5h": "5h auto-reset threshold (%)",
        "threshold7d": "7d auto-reset threshold (%)",
        "thresholdHint": "Each window is evaluated independently. Enter 0.1–100; both default to 100.",
        "thresholdInvalid": "Automatic reset-credit thresholds must be between 0.1% and 100%."
      },
      "expiresAtTimezoneHint": "Input is interpreted in your browser time zone ({timezone}).",
      "oauth": {
        "grok": {
          "emailPasswordAuth": "Email + password",
          "emailPasswordDesc": "Sign in with a Grok web email and password. The server uses the password only to obtain an ephemeral SSO cookie, then converts it to Build OAuth credentials. Neither the password nor raw SSO is stored on the account.",
          "emailPasswordInputLabel": "email----password",
          "emailPasswordPlaceholder": "user{'@'}example.com----your-password\nMultiple lines supported",
          "emailPasswordHint": "Format: email----password (password may contain -). Requires YesCaptcha keys; use a matching-region proxy when needed.",
          "pleaseEnterPassword": "Please enter email----password (one per line)",
          "pleaseEnterSSOToken": "Please enter an SSO token",
          "failedToValidateSSO": "Failed to validate Grok SSO",
          "failedToAuthorizePassword": "Grok password authorization failed"
        }
      },
      "errorPrefix": "Error: {message}",
      "imagePreviewAlt": "Test image {index}",
      "imageLightboxAlt": "Image preview",
      "videoTestMode": "Mode: Video generation test",
      "videoPreview": "Generated video:",
      "videoReceived": "Received test video #{count}",
      "usageWindow": {
        "grokUsed": "Used $",
        "grokBalance": "Bal $",
        "grokPrepaid": "Prepaid balance",
        "grokMonthlyLimit": "Monthly used / limit (USD)",
        "grokOverage": "Overage onDemandUsed/onDemandCap",
        "grokOverageShort": "OD $",
        "estimatedTotalCost": "Est. total ${cost}",
        "estimatedTotalCostTooltip": "Estimated total cost at 100% utilization, based on current window cost and utilization"
      },
      "openaiQuotaReset": {
        "autoStatus": {
          "checking": "Checking",
          "available": "Credit available",
          "resetting": "Auto-resetting",
          "success": "Auto-reset succeeded",
          "noCredit": "No credit",
          "failed": "Auto-reset failed"
        }
      }
    },
    "redeem": {
      "expiryDateRequired": "Please enter a valid expiry date and time",
      "localTimeZoneHint": "Custom time is interpreted in your browser time zone ({timezone})."
    },
    "announcements": {
      "createFirstAnnouncement": "No announcements yet. Create your first one."
    },
    "usage": {
      "upstreamRequestId": "Upstream ID",
      "upstreamRequestIdCopied": "Upstream ID copied"
    }
  }
}
