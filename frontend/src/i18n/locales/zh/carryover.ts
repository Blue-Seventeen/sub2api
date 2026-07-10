// Locale keys left behind during the upstream split-locale migration.
// Keep current split modules authoritative by merging this file before domain modules.
export default {
  "admin": {
    "accounts": {
      "audioPreview": "语音合成结果：",
      "audioReceived": "已收到第 {count} 段测试音频",
      "autoBestProxy": "自动选择最优代理",
      "autoBestProxyDesc": "自动使用当前健康且延迟最低的代理",
      "autoOps": "自动运维",
      "autoOpsDialog": {
        "action": {
          "delete_account": "删除账号",
          "disable_schedulable": "暂停调度",
          "enable_schedulable": "启用调度",
          "recover_state": "恢复状态",
          "recover_state_enable_schedulable": "恢复状态 + 启用调度",
          "refresh_token": "刷新令牌",
          "retest": "重新测试"
        },
        "actionResultLabel": "处置结果",
        "addCondition": "新增条件",
        "addCustomModel": "添加自定义模型",
        "addRule": "新增规则",
        "addSystemModel": "添加系统模型",
        "addTargetRule": "新增对象规则",
        "automaticRun": "自动",
        "clear": "清空",
        "collapse": "收起",
        "columns": {
          "action": "处置手段",
          "matchType": "匹配方式",
          "name": "规则名称",
          "operation": "操作",
          "pattern": "匹配内容",
          "priority": "优先级",
          "subject": "规则对象"
        },
        "configStatus": "配置状态",
        "configured": "已保存",
        "customModelPlaceholder": "输入自定义模型 ID",
        "edit": {
          "action": "处置手段",
          "matchType": "匹配规则",
          "name": "规则名称",
          "namePlaceholder": "例如：测试连接命中 deactivated 则暂停调度",
          "pattern": "匹配内容",
          "patternPlaceholder": "输入关键词或子串",
          "priority": "优先级",
          "subject": "规则对象"
        },
        "enabledHint": "保存后立即执行一次，之后按间隔轮询。",
        "enabledLabel": "启用自动运维",
        "intervalHint": "请输入正整数分钟。自动触发只对全局符合条件的账号生效。",
        "intervalLabel": "自动触发间隔（分钟）",
        "intervalPlaceholder": "例如 10",
        "logCount": "运维记录",
        "logsDescription": "只展示命中规则的步骤；记录仅保留 24 小时。",
        "logsEmpty": "暂无运维记录",
        "logsTitle": "运维记录",
        "manualRun": "手动",
        "matchType": {
          "contains": "包含",
          "not_contains": "不包含"
        },
        "matchedRuleLabel": "命中规则",
        "matchedStepsCount": "命中 {count} 条",
        "modelsDescription": "自动运维执行“重新测试”时，会按平台配置的模型列表依次尝试。",
        "modelsEmpty": "未配置模型，届时会使用系统默认测试模型。",
        "modelsTitle": "测试模型配置",
        "noMatchedRules": "本次运维无规则命中",
        "notConfigured": "未保存",
        "notContainsLabel": "未出现关键词：{pattern}",
        "responseLabel": "触发响应",
        "ruleCount": "规则数量",
        "rulesDescription": "规则按优先级从小到大执行，拖拽后会按当前顺序重写优先级。",
        "rulesEmpty": "暂无规则，未命中账号名称规则时，将默认执行重新测试。",
        "rulesTitle": "规则编排",
        "runSummary": "请求 {total} 个账号，符合条件 {eligible} 个，已完成 {completed} 个",
        "runtimeTitle": "运行状态",
        "sampleCount": "响应样本",
        "sampleOccurrences": "出现 {count} 次",
        "samplesDescription": "展示近 24 小时自动运维捕获的测试或刷新响应去重结果。",
        "samplesEmpty": "暂无响应样本",
        "samplesTitle": "响应样本",
        "selectSystemModel": "从系统模型中选择",
        "statusTitle": "当前状态",
        "subject": {
          "account_name": "账号名称",
          "refresh_response": "刷新令牌",
          "test_response": "测试连接"
        },
        "summaryDescription": "对象规则决定哪些账号会进入自动运维。只有命中“自动运维接管”的规则，才会继续执行现有运维动作流程。",
        "summaryTitle": "全局自动运维配置",
        "targetAccountStatus": {
          "error": "错误",
          "normal": "正常",
          "paused": "暂停",
          "rate_limited": "限流中",
          "temp_unschedulable": "临时不可调度"
        },
        "targetAction": {
          "manual": "人工运维",
          "takeover": "自动运维接管"
        },
        "targetBoolean": {
          "false": "未启用",
          "true": "启用"
        },
        "targetColumns": {
          "action": "处置手段",
          "conditions": "匹配条件",
          "name": "规则名称",
          "operation": "操作",
          "priority": "优先级"
        },
        "targetEdit": {
          "action": "处置手段",
          "conditions": "匹配条件",
          "field": "字段",
          "name": "规则名称",
          "namePlaceholder": "例如：错误且调度开启",
          "operator": "运算符",
          "priority": "优先级",
          "value": "匹配值",
          "valuePlaceholder": "请输入匹配值"
        },
        "targetField": {
          "account_name": "账号名称",
          "account_status": "账号状态",
          "auth_type": "认证类型",
          "group": "账号分组",
          "last_used_days": "最近使用天数",
          "platform": "账号平台",
          "schedulable": "调度状态"
        },
        "targetGroup": {
          "ungrouped": "未分组"
        },
        "targetLastUsedDays": {
          "hint": "匹配 N 天前及更早的账号；从未使用过的账号也会视为命中。",
          "placeholder": "例如 8",
          "summary": "{days} 天前及更早"
        },
        "targetOperator": {
          "contains": "包含",
          "eq": "==",
          "neq": "!=",
          "not_contains": "不包含"
        },
        "targetRuleCount": "对象规则",
        "targetRulesDescription": "对象规则决定账号是否进入自动运维。只有“自动运维接管”才会进入后续动作流程。",
        "targetRulesEmpty": "暂无对象规则。当前不会接管任何账号，只有命中“自动运维接管”规则的账号才会进入自动运维。",
        "targetRulesTitle": "自动运维对象配置",
        "title": "自动运维",
        "toast": {
          "loadFailed": "加载自动运维数据失败",
          "saveFailed": "保存自动运维配置失败",
          "saveSuccess": "自动运维配置已保存",
          "validationFailed": "请完整填写规则的必填项"
        },
        "unnamedRule": "未命名规则",
        "unnamedTargetRule": "未命名对象规则"
      },
      "autoOpsManualFailed": "触发自动运维失败",
      "autoOpsManualNoEligible": "已触发自动运维，但没有符合条件的账号",
      "autoOpsManualTriggered": "已触发自动运维，运行 #{runId}，符合条件账号 {count} 个",
      "bulkActions": {
        "autoOps": "自动运维",
        "editFiltered": "批量编辑筛选结果"
      },
      "fromModel": "原模型",
      "messages": {
        "accountCreated": "账号创建成功"
      },
      "oauth": {
        "openai": {
          "accessTokenAuth": "Access Token 授权",
          "mobileRefreshTokenAuth": "移动端 Refresh Token 授权"
        }
      },
      "platforms": {
        "ali": "Qwen/阿里",
        "deepseek": "DeepSeek",
        "kling": "Kling",
        "midjourney": "Midjourney",
        "mistral": "Mistral",
        "moonshot": "Kimi/月之暗面",
        "openrouter": "OpenRouter",
        "perplexity": "Perplexity",
        "siliconflow": "SiliconFlow",
        "suno": "Suno",
        "volcengine": "火山方舟/豆包",
        "xai": "xAI",
        "zhipu": "GLM/智谱"
      },
      "sendingASRRequest": "发送 ASR 转写测试请求...",
      "sendingEmbeddingRequest": "发送 Embedding 测试请求...",
      "sendingRerankRequest": "发送 Rerank 测试请求...",
      "sendingTTSRequest": "发送 TTS 语音测试请求...",
      "sendingTaskRequest": "提交 New-API 任务测试...",
      "sendingVideoRequest": "发送视频理解测试请求...",
      "stats": {
        "loadFailed": "账号使用统计加载失败，请稍后重试或检查后端日志"
      },
      "taskPromptDefault": "A cute orange cat astronaut sticker.",
      "taskPromptLabel": "任务提示词",
      "taskPromptPlaceholder": "例如：A cute orange cat astronaut sticker.",
      "taskTestHint": "该测试会提交 Suno/Kling/Midjourney 任务，并最多轮询 60 秒。",
      "testTypeCostHint": "该探针会真实发送上游多模态或任务请求，可能产生费用。",
      "testTypeLabel": "测试类型",
      "testTypeSummary": "类型：{type}",
      "testTypes": {
        "asr": "ASR",
        "auto": "自动",
        "embedding": "Embedding",
        "image": "图片",
        "rerank": "Rerank",
        "task": "Task",
        "text": "文本",
        "tts": "TTS",
        "video": "视频理解"
      },
      "toModel": "目标模型",
      "ttsPromptDefault": "Say hi from sub2api.",
      "ttsPromptLabel": "TTS 输入文本",
      "ttsPromptPlaceholder": "例如：Say hi from sub2api.",
      "ttsTestHint": "该测试会发送极短文本转语音请求，并验证返回非空音频。",
      "ttsVoiceHint": "不同平台允许的音色不同。TTS 测试成功后会按平台保存在本机浏览器，下次可直接选择。",
      "ttsVoiceLabel": "TTS 音色",
      "ttsVoicePlaceholder": "例如：alloy / tongtong / 自定义音色 ID",
      "vertex": {
        "anthropicDesc": "使用 Google Cloud Service Account JSON 通过 Vertex AI 调用 Anthropic Claude。",
        "chooseJson": "选择 JSON",
        "clientEmailMissing": "Service Account JSON 缺少 client_email",
        "createJsonHint": "上传或拖入 JSON 后会自动读取 project_id，密钥内容仅用于创建账号提交。",
        "dropJson": "拖入服务账号 JSON",
        "editJsonHint": "Service Account JSON 不在编辑页显示；需要更换 JSON 时请删除账号后重新创建。",
        "geminiDesc": "使用 Google Cloud Service Account JSON 访问 Vertex AI Gemini。建议将 Vertex 账号放入独立分组，避免和 AI Studio/Gemini OAuth 同模型混调。",
        "jsonDropHint": "将 .json 文件拖到这里，或点击按钮选择文件。",
        "jsonHiddenHint": "密钥内容不会在表单中显示。",
        "jsonInvalid": "Service Account JSON 格式无效",
        "jsonLoaded": "已读取服务账号 JSON",
        "jsonMissingFields": "Service Account JSON 缺少 project_id、client_email 或 private_key",
        "jsonRequired": "请上传 Service Account JSON",
        "location": "Location",
        "locationHint": "不同 Vertex 模型可用 location 可能不同，这里选择账号默认 endpoint location。",
        "locationRequired": "请选择 Vertex location",
        "projectId": "Project ID",
        "projectIdMissing": "Service Account JSON 缺少 project_id",
        "projectPlaceholder": "从 JSON 自动读取",
        "serviceAccountJson": "服务账号 JSON"
      },
      "videoPromptDefault": "请同时根据画面和声音，简要说明视频里发生了什么，并只回答一句中文。",
      "videoPromptLabel": "视频理解提示词",
      "videoPromptPlaceholder": "例如：请同时根据画面和声音，简要说明视频里发生了什么。",
      "videoTestHint": "该测试会把内置 MP4 样本发送给支持视频理解的 Chat Completions 模型。"
    },
    "channels": {
      "billingMode": {
        "character": "TTS（按字符）",
        "duration": "ASR（按秒）"
      },
      "emptyModelsInPricing": "{platform} 渠道定价中未填写模型",
      "form": {
        "characterBillingHint": "TTS 按请求文本字符数计费，未提取到文本时按 1000 字符兜底。",
        "characterThousandPrice": "每 1000 字符价格",
        "durationBillingHint": "ASR 按响应或上传音频时长计费，缺失时按 1 秒兜底。",
        "durationSecondPrice": "每秒价格",
        "unitPriceRequired": "ASR/TTS 计费模式必须设置单位价格"
      },
      "noGroupsSelected": "{platform} 渠道定价至少选择一个分组"
    },
    "groups": {
      "modelsList": {
        "add": "新增",
        "delete": "删除",
        "edit": "编辑",
        "modelPlaceholder": "模型名称，例如 kimi-k2.6 或 kimi-*",
        "selectedCount": "已选 {selected} / {total}"
      },
      "peakRate": {
        "addWindow": "添加时段",
        "errors": {
          "maxWindows": "高峰时段最多 24 段"
        }
      },
      "subscription": {
        "customLimit": "自定义限额（USD）",
        "customWindowHours": "自定义窗口（小时）"
      }
    },
    "ops": {
      "errorDetail": {
        "apiKeyName": "Key 名称"
      },
      "runtime": {
        "metricThresholds": "指标阈值配置",
        "metricThresholdsHint": "配置各项指标的告警阈值，超出阈值时将以红色显示",
        "requestErrorRateMaxPercent": "请求错误率最大值（%）",
        "requestErrorRateMaxPercentHint": "请求错误率高于此值时显示为红色（默认：5%）",
        "slaMinPercent": "SLA 最低百分比",
        "slaMinPercentHint": "SLA 低于此值时显示为红色（默认：99.5%）",
        "ttftP99MaxMs": "TTFT P99 最大值（毫秒）",
        "ttftP99MaxMsHint": "TTFT P99 高于此值时显示为红色（默认：500ms）",
        "upstreamErrorRateMaxPercent": "上游错误率最大值（%）",
        "upstreamErrorRateMaxPercentHint": "上游错误率高于此值时显示为红色（默认：5%）"
      }
    },
    "proxies": {
      "autoProbe": {
        "button": "自动检测",
        "currentProxy": "当前检测代理 ID：{id}",
        "defaultInterval": "默认测试间隔（秒）",
        "defaultIntervalHint": "成功队列使用该间隔自动检测。",
        "enable": "启用自动检测",
        "enableHint": "由服务端后台定时检测所有代理，服务重启后会自动恢复。",
        "invalidDefaultInterval": "默认测试间隔必须大于或等于 1 秒",
        "invalidRetryInterval": "失败后重测间隔必须大于或等于 1 秒",
        "invalidStickyTTL": "粘性保持 TTL 必须大于或等于 1 秒",
        "loadFailed": "加载自动检测配置失败",
        "retryInterval": "失败后重测间隔（秒）",
        "retryIntervalHint": "失败队列使用该间隔自动重试。",
        "running": "自动检测运行中",
        "runningWithProxy": "自动检测运行中（代理 #{id}）",
        "saveFailed": "保存自动检测配置失败",
        "saved": "自动检测设置已保存",
        "stickyEnable": "保持自动代理粘性",
        "stickyEnableHint": "启用自动选择代理的账号会尽量复用同一个代理，直到该代理不可用。",
        "stickyTTL": "粘性保持 TTL（秒）",
        "stickyTTLHint": "账号与代理绑定在 Redis 中保留的时间。",
        "stopped": "自动检测已停止",
        "summary": "成功队列 {success} 个，失败队列 {failed} 个",
        "title": "自动检测设置"
      },
      "clash": {
        "create": "创建订阅",
        "createFailed": "创建 Clash 订阅失败",
        "created": "Clash 订阅已创建",
        "editManagedHint": "这是 Clash 订阅托管代理，此处只允许修改名称和状态。",
        "import": "导入 Clash 订阅",
        "invalidRefreshInterval": "刷新间隔至少为 60 秒",
        "invalidSubscriptionUrl": "订阅 URL 必须是有效的 http/https 地址",
        "invalidTestUrl": "测试 URL 必须是有效的 http/https 地址",
        "localRuntime": "本节点 mihomo 运行端口",
        "managedAuth": "由 mihomo 托管",
        "managedBadge": "托管",
        "namePlaceholder": "托管代理名称",
        "refresh": "刷新",
        "refreshFailed": "刷新订阅失败",
        "refreshInterval": "刷新间隔（秒）",
        "refreshRequested": "已触发订阅刷新",
        "runtimeUnavailable": "本节点托管运行态尚未就绪",
        "runtimeUnknown": "运行态未知",
        "subscriptionUrl": "订阅 URL",
        "testUrl": "测试 URL",
        "urlHint": "仅允许 http/https Clash 订阅 URL；该 URL 仅按管理员凭据语义保存。"
      },
      "columns": {
        "activeEgressAccounts": "实时使用"
      }
    },
    "settings": {
      "customMenu": {
        "openInNewTab": "在新页面打开",
        "openInNewTabHint": "开启后，点击这个侧边栏菜单会直接在浏览器新页面打开目标 URL，而不是在站内 iframe 中加载。"
      },
      "openaiFastPolicy": {
        "flexForcedHint": "系统强制不允许命中 flex 低价模式：用户传入 flex 时会自动移除 service_tier，按标准模式转发与计费。",
        "saveFailed": "保存 OpenAI Fast/Flex 策略失败",
        "saved": "OpenAI Fast/Flex 策略保存成功",
        "tierAuto": "auto",
        "tierDefault": "default",
        "tierScale": "scale"
      },
      "registration": {
        "invitationCodeMissingPrompt": "未填写邀请码时的提示内容",
        "invitationCodeMissingPromptHint": "当用户未填写邀请码就提交注册时，会弹出此提示框。支持嵌入 HTML（例如链接）。",
        "invitationCodeMissingPromptPlaceholder": "<p>请先前往 <a href=\"https://example.com\" target=\"_blank\">这里</a> 获取邀请码。</p>",
        "invitationCodeMissingPromptPreview": "前台弹窗预览",
        "invitationCodeMissingPromptWarning": "此内容会以 HTML 原样渲染给访客，请仅填写你信任的内容。"
      },
      "site": {
        "displayCurrencySymbol": "货币符号",
        "displayCurrencySymbolHint": "仅影响前端金额展示，不改变余额、计费、支付、退款等后端数值逻辑",
        "displayCurrencySymbolLocalOnly": "货币符号仅用于本机",
        "displayCurrencySymbolLocalOnlyHint": "开启后保存到当前服务实例本机配置文件，不写入 PostgreSQL；关闭后保存到数据库并在共库节点共享",
        "displayCurrencySymbolPlaceholder": "$ / ¥ / RMB"
      }
    },
    "subscriptions": {
      "adjustAtLeastOne": "请至少填写一个要调整的字段",
      "adjustDaysNonZero": "调整天数不能为 0；不调整天数请留空",
      "adjustUsageHint": "留空表示不修改该窗口；允许设置超过限额的已用值，用于人工封顶或阻断。",
      "adjustUsageTitle": "调整已用额度",
      "columns": {
        "redeemCode": "兑换码",
        "starts": "开始时间"
      },
      "copyFields": {
        "email": "邮箱",
        "expiresAt": "到期时间",
        "group": "分组",
        "redeemCode": "兑换码",
        "startsAt": "开始时间",
        "status": "状态",
        "usage": "用量"
      },
      "copyRecord": "复制",
      "copySuccess": "订阅信息已复制",
      "custom": "{hours}H",
      "failedToDelete": "永久删除订阅失败",
      "form": {
        "customUsage": "自定义窗口已用额度",
        "dailyUsage": "每日已用额度",
        "monthlyUsage": "每月已用额度",
        "weeklyUsage": "每周已用额度"
      },
      "guide": {
        "actions": {
          "hardDelete": "删除",
          "hardDeleteDesc": "在需要清理时硬删除已撤销或已过期订阅记录",
          "restore": "恢复",
          "restoreDesc": "恢复已撤销订阅，并按到期时间重新归类"
        }
      },
      "hardDeleteConfirm": "确定要永久删除 '{user}' 的订阅记录吗？这是硬删除，删除后不可恢复。",
      "hardDeleteSubscription": "永久删除订阅",
      "invalidUsage": "已用额度必须为非负数",
      "reactivateBlockedHint": "这类历史记录仅保留证据展示与永久删除操作。如需继续给用户授权，请重新分配一个有效订阅分组。",
      "reactivateBlockedMessage": "该订阅关联的真实分组 '{group}' 已不存在、已禁用或不再是订阅类型，不能恢复为生效中，也不能通过调整时间重新激活。",
      "reactivateBlockedTitle": "无法恢复订阅",
      "reactivateBlockedToast": "该订阅关联的分组已失效，不能恢复或调整为生效状态",
      "resetQuotaAtLeastOne": "请至少选择一个要重置的配额窗口",
      "resetQuotaHint": "默认全选等价于旧版全量重置。",
      "resetWindows": {
        "custom": "自定义窗口用量",
        "daily": "每日用量",
        "monthly": "每月用量",
        "weekly": "每周用量"
      },
      "subscriptionDeleted": "订阅已永久删除"
    },
    "usage": {
      "billingModeCharacter": "TTS（按字符）",
      "billingModeDuration": "ASR（按秒）"
    },
    "users": {
      "balanceOperationsUseReal": "本次操作按真实余额结算",
      "columns": {
        "displayBalance": "显示余额",
        "realBalance": "真实余额"
      },
      "displayBalanceLabel": "显示余额",
      "passwordCopied": "密码已复制",
      "realBalanceLabel": "真实余额"
    }
  },
  "auth": {
    "invitationCodePromptTitle": "请输入邀请码后再注册"
  },
  "common": {
    "apply": "应用",
    "clear": "清除",
    "creating": "创建中...",
    "peakRateCompactMultiple": "高峰 {count} 段",
    "peakRateCompactSingle": "高峰 x{multiplier}",
    "peakRateFormula": "{window} 基础倍率 x 高峰倍率 = {base} x {peak} = {final}（最终倍率）",
    "required": "必填",
    "sending": "发送中...",
    "tryAgain": "请重试"
  },
  "customPage": {
    "autoOpenDesc": "如果浏览器没有自动打开，请点击下方按钮继续。",
    "autoOpenTitle": "正在为你打开新页面"
  },
  "keyUsage": {
    "limitCustom": "自定义限额"
  },
  "nav": {
    "promotion": "推广中心",
    "promotionAdmin": "推广管理"
  },
  "payment": {
    "admin": {
      "customLimit": "自定义限额"
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
    "balanceAddedPromotion": "推广收益发放",
    "balanceDeductedPromotion": "推广收益冲回"
  },
  "usage": {
    "characterCount": "字符数",
    "characterTotalPrice": "字符总价",
    "characterUnit": "字符",
    "characterUnitPrice": "每 1000 字符价格",
    "durationSeconds": "计费时长",
    "durationTotalPrice": "时长总价",
    "durationUnitPrice": "每秒价格"
  },
  "userSubscriptions": {
    "custom": "{hours}H"
  }
} as const
