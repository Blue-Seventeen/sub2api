// Locale keys left behind during the upstream split-locale migration.
// Keep current split modules authoritative by merging this file before domain modules.
export default {
  "admin": {
    "accounts": {
      "audioPreview": "Generated audio:",
      "audioReceived": "Received test audio #{count}",
      "autoBestProxy": "Auto Select Best Proxy",
      "autoBestProxyDesc": "Automatically use the healthiest proxy with the lowest latency",
      "autoOps": "Auto Ops",
      "autoOpsDialog": {
        "action": {
          "delete_account": "Delete Account",
          "disable_schedulable": "Pause Scheduling",
          "enable_schedulable": "Enable Scheduling",
          "recover_state": "Recover State",
          "recover_state_enable_schedulable": "Recover State + Enable Scheduling",
          "refresh_token": "Refresh Token",
          "retest": "Retest"
        },
        "actionResultLabel": "Action Result",
        "addCondition": "Add Condition",
        "addCustomModel": "Add Custom Model",
        "addRule": "Add Rule",
        "addSystemModel": "Add Built-in Model",
        "addTargetRule": "Add Target Rule",
        "automaticRun": "Automatic",
        "clear": "Clear",
        "collapse": "Collapse",
        "columns": {
          "action": "Action",
          "matchType": "Match",
          "name": "Rule Name",
          "operation": "Actions",
          "pattern": "Pattern",
          "priority": "Priority",
          "subject": "Rule Subject"
        },
        "configStatus": "Configuration",
        "configured": "Saved",
        "customModelPlaceholder": "Enter custom model ID",
        "edit": {
          "action": "Action",
          "matchType": "Match Type",
          "name": "Rule Name",
          "namePlaceholder": "e.g. Pause scheduling when test response contains deactivated",
          "pattern": "Pattern",
          "patternPlaceholder": "Enter keyword or substring",
          "priority": "Priority",
          "subject": "Rule Subject"
        },
        "enabledHint": "Run once immediately after saving, then continue on the configured interval.",
        "enabledLabel": "Enable Auto Ops",
        "intervalHint": "Use a positive integer in minutes. Automatic runs only apply to globally eligible accounts.",
        "intervalLabel": "Automatic Interval (minutes)",
        "intervalPlaceholder": "e.g. 10",
        "logCount": "Runs",
        "logsDescription": "Only matched rules are displayed. Logs are retained for 24 hours.",
        "logsEmpty": "No auto ops logs yet",
        "logsTitle": "Ops Records",
        "manualRun": "Manual",
        "matchType": {
          "contains": "Contains",
          "not_contains": "Not Contains"
        },
        "matchedRuleLabel": "Matched Rule",
        "matchedStepsCount": "Matched {count} step(s)",
        "modelsDescription": "When auto ops performs retest, models are tried in order by platform.",
        "modelsEmpty": "No models configured. The system default test model will be used.",
        "modelsTitle": "Test Model Configuration",
        "noMatchedRules": "No rule matched in this run",
        "notConfigured": "Not Saved",
        "notContainsLabel": "Keyword not present: {pattern}",
        "responseLabel": "Response",
        "ruleCount": "Rules",
        "rulesDescription": "Rules are evaluated in ascending priority order. Dragging rewrites priorities by order.",
        "rulesEmpty": "No rules configured. If account name rules do not match, auto ops will retest by default.",
        "rulesTitle": "Rule Orchestration",
        "runSummary": "Requested {total} account(s), eligible {eligible}, completed {completed}",
        "runtimeTitle": "Runtime",
        "sampleCount": "Samples",
        "sampleOccurrences": "Seen {count} times",
        "samplesDescription": "Deduplicated test/refresh samples captured in the last 24 hours.",
        "samplesEmpty": "No response samples yet",
        "samplesTitle": "Response Samples",
        "selectSystemModel": "Select a built-in model",
        "statusTitle": "Current Status",
        "subject": {
          "account_name": "Account Name",
          "refresh_response": "Refresh Response",
          "test_response": "Test Response"
        },
        "summaryDescription": "Target rules decide which accounts enter auto ops. Only rules marked as takeover continue into the existing action flow.",
        "summaryTitle": "Global Auto Ops Configuration",
        "targetAccountStatus": {
          "error": "Error",
          "normal": "Normal",
          "paused": "Paused",
          "rate_limited": "Rate Limited",
          "temp_unschedulable": "Temp Unschedulable"
        },
        "targetAction": {
          "manual": "Manual Handling",
          "takeover": "Auto Ops Takeover"
        },
        "targetBoolean": {
          "false": "Disabled",
          "true": "Enabled"
        },
        "targetColumns": {
          "action": "Disposition",
          "conditions": "Conditions",
          "name": "Rule Name",
          "operation": "Actions",
          "priority": "Priority"
        },
        "targetEdit": {
          "action": "Disposition",
          "conditions": "Conditions",
          "field": "Field",
          "name": "Rule Name",
          "namePlaceholder": "e.g. Error and scheduling enabled",
          "operator": "Operator",
          "priority": "Priority",
          "value": "Value",
          "valuePlaceholder": "Enter match value"
        },
        "targetField": {
          "account_name": "Account Name",
          "account_status": "Account Status",
          "auth_type": "Auth Type",
          "group": "Group",
          "last_used_days": "Last Used Days",
          "platform": "Platform",
          "schedulable": "Scheduling Status"
        },
        "targetGroup": {
          "ungrouped": "Ungrouped"
        },
        "targetLastUsedDays": {
          "hint": "Matches accounts last used N days ago or earlier. Accounts never used also count as matched.",
          "placeholder": "e.g. 8",
          "summary": "{days} day(s) ago or earlier"
        },
        "targetOperator": {
          "contains": "Contains",
          "eq": "==",
          "neq": "!=",
          "not_contains": "Not Contains"
        },
        "targetRuleCount": "Target Rules",
        "targetRulesDescription": "Target rules decide whether an account enters auto ops. Only takeover rules continue to the existing action flow.",
        "targetRulesEmpty": "No target rules configured. Auto ops will not take over any account until a takeover rule matches.",
        "targetRulesTitle": "Auto Ops Target Configuration",
        "title": "Auto Ops",
        "toast": {
          "loadFailed": "Failed to load auto ops data",
          "saveFailed": "Failed to save auto ops configuration",
          "saveSuccess": "Auto ops configuration saved",
          "validationFailed": "Please complete all required rule fields"
        },
        "unnamedRule": "Unnamed Rule",
        "unnamedTargetRule": "Unnamed Target Rule"
      },
      "autoOpsManualFailed": "Failed to trigger auto ops",
      "autoOpsManualNoEligible": "Auto ops triggered, but no eligible accounts were found",
      "autoOpsManualTriggered": "Auto ops triggered. Run #{runId}, eligible accounts: {count}",
      "bulkActions": {
        "autoOps": "Auto Ops",
        "editFiltered": "Bulk Edit Filtered"
      },
      "fromModel": "From model",
      "messages": {
        "accountCreated": "Account created successfully"
      },
      "oauth": {
        "openai": {
          "accessTokenAuth": "Access Token auth",
          "mobileRefreshTokenAuth": "Mobile Refresh Token auth"
        }
      },
      "platforms": {
        "ali": "Qwen/Ali",
        "deepseek": "DeepSeek",
        "kling": "Kling",
        "midjourney": "Midjourney",
        "mistral": "Mistral",
        "moonshot": "Kimi/Moonshot",
        "openrouter": "OpenRouter",
        "perplexity": "Perplexity",
        "siliconflow": "SiliconFlow",
        "suno": "Suno",
        "volcengine": "VolcEngine/Doubao",
        "xai": "xAI",
        "zhipu": "GLM/Zhipu"
      },
      "sendingASRRequest": "Sending ASR transcription test request...",
      "sendingEmbeddingRequest": "Sending embedding test request...",
      "sendingRerankRequest": "Sending rerank test request...",
      "sendingTTSRequest": "Sending TTS speech test request...",
      "sendingTaskRequest": "Submitting New-API task test...",
      "sendingVideoRequest": "Sending video understanding test request...",
      "stats": {
        "loadFailed": "Failed to load account usage statistics. Please try again or check backend logs."
      },
      "taskPromptDefault": "A cute orange cat astronaut sticker.",
      "taskPromptLabel": "Task prompt",
      "taskPromptPlaceholder": "Example: A cute orange cat astronaut sticker.",
      "taskTestHint": "This test submits a Suno/Kling/Midjourney task and polls for up to 60 seconds.",
      "testTypeCostHint": "This probe sends a real upstream multimodal or task request and may incur cost.",
      "testTypeLabel": "Test type",
      "testTypeSummary": "Type: {type}",
      "testTypes": {
        "asr": "ASR",
        "auto": "Auto",
        "embedding": "Embedding",
        "image": "Image",
        "rerank": "Rerank",
        "task": "Task",
        "text": "Text",
        "tts": "TTS",
        "video": "Video understanding"
      },
      "toModel": "To model",
      "ttsPromptDefault": "Say hi from sub2api.",
      "ttsPromptLabel": "TTS input text",
      "ttsPromptPlaceholder": "Example: Say hi from sub2api.",
      "ttsTestHint": "This test sends a short text-to-speech request and validates non-empty audio output.",
      "ttsVoiceHint": "Voice names differ by platform. A successful TTS test saves this voice locally for this platform.",
      "ttsVoiceLabel": "TTS voice",
      "ttsVoicePlaceholder": "Example: alloy / tongtong / custom voice id",
      "vertex": {
        "anthropicDesc": "Use Google Cloud Service Account JSON to call Anthropic Claude through Vertex AI.",
        "chooseJson": "Choose JSON",
        "clientEmailMissing": "Service Account JSON is missing client_email",
        "createJsonHint": "After uploading or dropping JSON, project_id is read automatically. Secret key content is only used when creating the account.",
        "dropJson": "Drop Service Account JSON",
        "editJsonHint": "Service Account JSON is not shown on the edit page. To replace it, delete the account and create it again.",
        "geminiDesc": "Use Google Cloud Service Account JSON to access Vertex AI Gemini. Put Vertex accounts in a dedicated group to avoid mixing them with AI Studio/Gemini OAuth accounts for the same model.",
        "jsonDropHint": "Drop a .json file here, or click the button to choose one.",
        "jsonHiddenHint": "Secret key content is not shown in the form.",
        "jsonInvalid": "Invalid Service Account JSON",
        "jsonLoaded": "Service Account JSON loaded",
        "jsonMissingFields": "Service Account JSON is missing project_id, client_email, or private_key",
        "jsonRequired": "Please upload Service Account JSON",
        "location": "Location",
        "locationHint": "Different Vertex models may require different locations. This selects the default endpoint location for this account.",
        "locationRequired": "Please select Vertex location",
        "projectId": "Project ID",
        "projectIdMissing": "Service Account JSON is missing project_id",
        "projectPlaceholder": "Read from JSON automatically",
        "serviceAccountJson": "Service Account JSON"
      },
      "videoPromptDefault": "Describe what happens in this video using both visual and audio cues. Answer briefly.",
      "videoPromptLabel": "Video understanding prompt",
      "videoPromptPlaceholder": "Example: Describe what happens in this video using both visual and audio cues.",
      "videoTestHint": "This test sends an embedded MP4 sample to a chat-compatible video understanding model."
    },
    "channels": {
      "billingMode": {
        "character": "TTS (Per Character)",
        "duration": "ASR (Per Second)"
      },
      "emptyModelsInPricing": "No models configured in {platform} channel pricing",
      "form": {
        "characterBillingHint": "TTS bills by request text characters. Falls back to 1000 characters when text is unavailable.",
        "characterThousandPrice": "Price per 1000 characters",
        "durationBillingHint": "ASR bills by response or uploaded audio duration. Falls back to 1 second when unavailable.",
        "durationSecondPrice": "Price per second",
        "unitPriceRequired": "Unit price is required for ASR/TTS billing mode"
      },
      "noGroupsSelected": "Select at least one group for {platform} channel pricing"
    },
    "groups": {
      "modelsList": {
        "add": "Add",
        "delete": "Delete",
        "edit": "Edit",
        "modelPlaceholder": "Model name, e.g. kimi-k2.6 or kimi-*",
        "selectedCount": "Selected {selected} / {total}"
      },
      "peakRate": {
        "addWindow": "Add window",
        "errors": {
          "maxWindows": "Peak windows can contain at most 24 entries"
        }
      },
      "subscription": {
        "customLimit": "Custom Limit (USD)",
        "customWindowHours": "Custom Window (Hours)"
      }
    },
    "ops": {
      "errorDetail": {
        "apiKeyName": "Key Name"
      },
      "runtime": {
        "metricThresholds": "Metric Thresholds",
        "metricThresholdsHint": "Configure alert thresholds for metrics; values above thresholds are shown in red",
        "requestErrorRateMaxPercent": "Request Error Rate Maximum (%)",
        "requestErrorRateMaxPercentHint": "Request error rate above this value is shown in red (default: 5%)",
        "slaMinPercent": "SLA Minimum Percentage",
        "slaMinPercentHint": "SLA below this value is shown in red (default: 99.5%)",
        "ttftP99MaxMs": "TTFT P99 Maximum (ms)",
        "ttftP99MaxMsHint": "TTFT P99 above this value is shown in red (default: 500ms)",
        "upstreamErrorRateMaxPercent": "Upstream Error Rate Maximum (%)",
        "upstreamErrorRateMaxPercentHint": "Upstream error rate above this value is shown in red (default: 5%)"
      }
    },
    "proxies": {
      "autoProbe": {
        "button": "Auto Probe",
        "currentProxy": "Current proxy ID: {id}",
        "defaultInterval": "Default interval (seconds)",
        "defaultIntervalHint": "Used for proxies in the success queue.",
        "enable": "Enable auto probe",
        "enableHint": "Run proxy checks in a server-side background worker and restore automatically after restart.",
        "invalidDefaultInterval": "Default interval must be at least 1 second",
        "invalidRetryInterval": "Retry interval must be at least 1 second",
        "invalidStickyTTL": "Sticky TTL must be at least 1 second",
        "loadFailed": "Failed to load auto probe settings",
        "retryInterval": "Retry interval after failure (seconds)",
        "retryIntervalHint": "Used for proxies in the failed queue.",
        "running": "Auto probe is running",
        "runningWithProxy": "Auto probe is running (proxy #{id})",
        "saveFailed": "Failed to save auto probe settings",
        "saved": "Auto probe settings saved",
        "stickyEnable": "Keep auto-selected proxy sticky",
        "stickyEnableHint": "Accounts using auto proxy selection will keep the same proxy until it becomes unavailable.",
        "stickyTTL": "Sticky TTL (seconds)",
        "stickyTTLHint": "How long an account-to-proxy binding is kept in Redis.",
        "stopped": "Auto probe is stopped",
        "summary": "Success queue {success}, failed queue {failed}",
        "title": "Auto Probe Settings"
      },
      "clash": {
        "create": "Create Subscription",
        "createFailed": "Failed to create Clash subscription",
        "created": "Clash subscription created",
        "editManagedHint": "This is a managed Clash subscription proxy. Only name and status can be edited here.",
        "import": "Import Clash Subscription",
        "invalidRefreshInterval": "Refresh interval must be at least 60 seconds",
        "invalidSubscriptionUrl": "Subscription URL must be a valid http/https URL",
        "invalidTestUrl": "Test URL must be a valid http/https URL",
        "localRuntime": "Local mihomo runtime",
        "managedAuth": "Managed by mihomo",
        "managedBadge": "Managed",
        "namePlaceholder": "Managed proxy name",
        "refresh": "Refresh",
        "refreshFailed": "Failed to refresh subscription",
        "refreshInterval": "Refresh Interval (seconds)",
        "refreshRequested": "Subscription refresh requested",
        "runtimeUnavailable": "Managed runtime is not ready on this node",
        "runtimeUnknown": "Runtime status unknown",
        "subscriptionUrl": "Subscription URL",
        "testUrl": "Test URL",
        "urlHint": "Only http/https Clash subscription URLs are accepted. The URL is stored for admins only."
      },
      "columns": {
        "activeEgressAccounts": "Live Use"
      }
    },
    "settings": {
      "customMenu": {
        "openInNewTab": "Open in New Tab",
        "openInNewTabHint": "When enabled, clicking this sidebar item will open the target URL directly in a new browser tab instead of loading it inside an iframe."
      },
      "openaiFastPolicy": {
        "flexForcedHint": "The system never allows flex low-cost mode to reach upstream. When a request sends flex, service_tier is removed and the request is forwarded and billed as standard mode.",
        "saveFailed": "Failed to save OpenAI Fast/Flex policy settings",
        "saved": "OpenAI Fast/Flex policy settings saved",
        "tierAuto": "auto",
        "tierDefault": "default",
        "tierScale": "scale"
      },
      "registration": {
        "invitationCodeMissingPrompt": "Prompt shown when invitation code is missing",
        "invitationCodeMissingPromptHint": "When users submit registration without an invitation code, this dialog will be shown. HTML is allowed (for example, links).",
        "invitationCodeMissingPromptPlaceholder": "<p>Please visit <a href=\"https://example.com\" target=\"_blank\">this page</a> to get an invitation code.</p>",
        "invitationCodeMissingPromptPreview": "Preview shown to users",
        "invitationCodeMissingPromptWarning": "This content is rendered as raw HTML for visitors. Only use trusted content."
      },
      "site": {
        "displayCurrencySymbol": "Currency Symbol",
        "displayCurrencySymbolHint": "Display only. Backend balances, billing, payments, and refunds keep their original numeric values.",
        "displayCurrencySymbolLocalOnly": "Currency Symbol Local Only",
        "displayCurrencySymbolLocalOnlyHint": "When enabled, saves to this service instance local config file without updating PostgreSQL. Disable it to share the symbol through the database.",
        "displayCurrencySymbolPlaceholder": "$ / ¥ / RMB"
      }
    },
    "subscriptions": {
      "adjustAtLeastOne": "Fill at least one field to adjust",
      "adjustDaysNonZero": "Adjustment days cannot be 0; leave it blank to keep days unchanged",
      "adjustUsageHint": "Leave blank to keep that window unchanged. Values may exceed the limit for manual capping or blocking.",
      "adjustUsageTitle": "Adjust Used Quota",
      "columns": {
        "redeemCode": "Redeem Code",
        "starts": "Starts"
      },
      "copyFields": {
        "email": "Email",
        "expiresAt": "Expiration Time",
        "group": "Group",
        "redeemCode": "Redeem Code",
        "startsAt": "Start Time",
        "status": "Status",
        "usage": "Usage"
      },
      "copyRecord": "Copy",
      "copySuccess": "Subscription copied",
      "custom": "{hours}H",
      "failedToDelete": "Failed to permanently delete subscription",
      "form": {
        "customUsage": "Custom window used quota",
        "dailyUsage": "Daily used quota",
        "monthlyUsage": "Monthly used quota",
        "weeklyUsage": "Weekly used quota"
      },
      "guide": {
        "actions": {
          "hardDelete": "Delete",
          "hardDeleteDesc": "Hard delete revoked or expired subscription evidence when cleanup is required",
          "restore": "Restore",
          "restoreDesc": "Restore a revoked subscription and reclassify it by expiration time"
        }
      },
      "hardDeleteConfirm": "Permanently delete the subscription record for '{user}'? This is a hard delete and cannot be recovered.",
      "hardDeleteSubscription": "Permanently Delete Subscription",
      "invalidUsage": "Used quota must be non-negative",
      "reactivateBlockedHint": "This historical record is kept only as evidence and may only be permanently deleted. Assign a valid subscription group if the user needs new access.",
      "reactivateBlockedMessage": "The real group linked to this subscription, '{group}', no longer exists, is disabled, or is no longer a subscription group. It cannot be restored to Active or reactivated by adjusting expiration.",
      "reactivateBlockedTitle": "Cannot Reactivate Subscription",
      "reactivateBlockedToast": "The linked group is no longer valid, so this subscription cannot be restored or reactivated",
      "resetQuotaAtLeastOne": "Select at least one quota window to reset",
      "resetQuotaHint": "All selected by default, which matches the old full reset behavior.",
      "resetWindows": {
        "custom": "Custom window usage",
        "daily": "Daily usage",
        "monthly": "Monthly usage",
        "weekly": "Weekly usage"
      },
      "subscriptionDeleted": "Subscription deleted permanently"
    },
    "usage": {
      "billingModeCharacter": "TTS (Per Character)",
      "billingModeDuration": "ASR (Per Second)"
    },
    "users": {
      "balanceOperationsUseReal": "This action uses real balance",
      "columns": {
        "displayBalance": "Display Balance",
        "realBalance": "Real Balance"
      },
      "displayBalanceLabel": "Display Balance",
      "passwordCopied": "Password copied",
      "realBalanceLabel": "Real Balance"
    }
  },
  "auth": {
    "invitationCodePromptTitle": "Enter an invitation code before registering"
  },
  "common": {
    "apply": "Apply",
    "clear": "Clear",
    "creating": "Creating...",
    "peakRateCompactMultiple": "Peak {count} windows",
    "peakRateCompactSingle": "Peak x{multiplier}",
    "peakRateFormula": "{window} base x peak = {base} x {peak} = {final} (final multiplier)",
    "required": "required",
    "sending": "Sending...",
    "tryAgain": "Please try again"
  },
  "customPage": {
    "autoOpenDesc": "If your browser does not open it automatically, click the button below.",
    "autoOpenTitle": "Opening a new page for you"
  },
  "keyUsage": {
    "limitCustom": "Custom Limit"
  },
  "nav": {
    "promotion": "Promotion Center",
    "promotionAdmin": "Promotion Admin"
  },
  "payment": {
    "admin": {
      "customLimit": "Custom Limit"
    },
    "planCard": {
      "customLimit": "{hours}H"
    }
  },
  "profile": {
    "authBindings": {
      "providers": {
        "github": "GitHub",
        "google": "Google"
      }
    }
  },
  "redeem": {
    "balanceAddedPromotion": "Promotion Reward Added",
    "balanceDeductedPromotion": "Promotion Reward Reversed"
  },
  "usage": {
    "characterCount": "Characters",
    "characterTotalPrice": "Character total price",
    "characterUnit": "chars",
    "characterUnitPrice": "Price per 1000 characters",
    "durationSeconds": "Duration",
    "durationTotalPrice": "Duration total price",
    "durationUnitPrice": "Price per second"
  },
  "userSubscriptions": {
    "custom": "{hours}H"
  }
} as const
