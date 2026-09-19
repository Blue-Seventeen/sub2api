// Messages added in upstream d681d079, kept separate from custom locale overrides.
export default {
  "keys": {
    "useKeyModal": {
      "deepseek": {
        "description": "通过当前 DeepSeek 分组配置 Claude Code、Codex 或 OpenCode。",
        "codexDescription": "使用 API Key 配置 Codex，并通过当前 DeepSeek 分组发送请求。",
        "codexConfigTomlHint": "下载下方模型目录，将两个文件保存到 Codex 配置目录后重启 Codex。",
        "codexNote": "启动 Codex 前先导出 SUB2API_API_KEY。下载的目录只包含模型元数据，不包含 API Key。"
      },
      "minimax": {
        "description": "通过当前 MiniMax 分组配置 Claude Code、Codex 或 OpenCode。",
        "codexDescription": "使用 API Key 配置 Codex，并通过当前 MiniMax 分组发送请求。",
        "codexConfigTomlHint": "下载下方模型目录，将两个文件保存到 Codex 配置目录后重启 Codex。",
        "codexNote": "启动 Codex 前先导出 SUB2API_API_KEY。下载的目录只包含模型元数据，不包含 API Key。"
      },
      "composite": {
        "description": "通过当前 Composite 路由分组配置受支持的客户端。",
        "codexDescription": "使用 API Key 和当前 Composite 分组的完整模型目录配置 Codex。",
        "codexConfigTomlHint": "下载下方模型目录，将两个文件保存到 Codex 配置目录后重启 Codex。",
        "codexNote": "启动 Codex 前先导出 SUB2API_API_KEY；分组会根据目录中选中的模型路由请求。"
      },
      "routedCodex": {
        "description": "使用当前路由分组的完整模型目录配置 Codex。",
        "configTomlHint": "下载下方模型目录，将两个文件保存到 Codex 配置目录后重启 Codex。",
        "note": "启动 Codex 前先导出 SUB2API_API_KEY。下载的目录只包含模型元数据，不包含 API Key。"
      },
      "codexModelCatalog": {
        "title": "Codex 模型目录",
        "description": "使用当前 API Key 获取目录，并保存到 config.toml 引用的路径。",
        "fetch": "获取目录",
        "retry": "重试",
        "download": "下载目录",
        "modelsCount": "已获取 {count} 个模型",
        "errorDescription": "无法使用当前 API Key 获取模型目录。"
      }
    }
  },
  "usage": {
    "requestedReasoningEffort": "请求推理强度",
    "nativeCompactionV2": "压缩",
    "compactionFilter": "请求类别",
    "allCompactionTypes": "全部请求",
    "compactionOnly": "仅原生压缩",
    "serviceTierUltrafast": "Ultrafast"
  },
  "monitorCommon": {
    "providers": {
      "antigravity": "Antigravity",
      "kimi": "Kimi",
      "zhipu": "智谱 GLM",
      "deepseek": "DeepSeek",
      "minimax": "MiniMax"
    },
    "checkMode": {
      "probe": "探活",
      "quota": "配额",
      "quota_probe": "探活 + 配额"
    },
    "quota": {
      "unavailable": "配额信息不可用",
      "windows": {
        "5h": "5 小时",
        "7d": "7 天",
        "7dSonnet": "7 天 Sonnet",
        "7dFable": "7 天 Fable",
        "weekly": "周",
        "daily": "日",
        "30d": "30 天",
        "total": "总量"
      },
      "labels": {
        "requests": "请求",
        "tokens": "Token",
        "shared": "共享",
        "pro": "Pro",
        "flash": "Flash"
      }
    }
  },
  "availableChannels": {
    "pricing": {
      "cacheWrite5mPrice": "缓存写入（5m）",
      "cacheWrite1hPrice": "缓存写入（1h）"
    }
  },
  "modelPlaza": {
    "detail": {
      "longContextDisabledNote": "该分组未启用长上下文阶梯计费，超阈值请求仍按基础档计费，官方阶梯仅供参考"
    },
    "table": {
      "cacheWriteShort": "写",
      "cacheReadShort": "读",
      "tierHint": "按单次请求的总上下文（输入 + 缓存写入 + 缓存读取）所在档位对整单计价",
      "tierHintMarginal": "仅超过阈值的部分按该档计价，输出不加价",
      "maxReasoningMultiplierBadge": "Max ×{multiplier}",
      "maxReasoningMultiplierHint": "最终转发的推理强度为 max 时，整次请求的计费与额度消耗乘以 {multiplier}",
      "marginalBadge": "超出部分计价",
      "timePricingRowHint": "按 {timezone} 时间，在该时段内发起的请求按本行价格计费",
      "timePricingRowHintWeekdays": "按 {timezone} 时间，仅工作日（周一至周五）在该时段内发起的请求按本行价格计费，周末全天按标准价",
      "timePricingRowHintPeak": "；本行价格未含高峰倍率，与高峰时段 {window} 重叠的部分实付再乘 ×{multiplier}",
      "timePricingWeekdays": "工作日",
      "timePricingRateHint": "生效倍率 {rate} × 时段倍率 {multiplier}"
    }
  },
  "admin": {
    "backup": {
      "columns": {
        "parts": "分卷数"
      },
      "actions": {
        "downloadParts": "下载分卷",
        "downloadPartsHint": "请按顺序下载全部分卷后拼接 gzip 字节流：Linux/macOS 使用 cat payload.part-* > backup.sql.gz；Windows 使用 copy /b payload.part-000001+payload.part-000002 backup.sql.gz。",
        "partLabel": "第 {index} 卷",
        "downloadFailed": "下载地址为空"
      }
    },
    "users": {
      "form": {
        "concurrencyPlaceholder": "0 表示不限制",
        "concurrencyHint": "该用户的最大并发请求数，0 = 不限制"
      },
      "concurrencyNonNegative": "并发数不能为负数，0 表示不限制",
      "restrictPublicGroups": "限制可访问的公开分组",
      "restrictPublicGroupsHint": "开启后，该用户仅能使用下方勾选的公开分组；关闭则可使用全部公开分组。",
      "publicGroupsRestricted": "公开分组（已限制）"
    },
    "groups": {
      "usageYesterday": "昨日",
      "form": {
        "maxReasoningEffortOverLimit": "超限访问控制",
        "maxReasoningEffortOverLimitDowngrade": "超过上限时自动降档",
        "maxReasoningEffortOverLimitDeny": "拒绝访问",
        "maxReasoningEffortOverLimitHint": "设置上限后生效。自动降档会将超过上限的请求改写为上限值后转发；拒绝访问则直接返回错误。",
        "reasoningEffortMappingsHint": "类型和模型均可留空，表示匹配全部模型。同一类型和模型下可添加多条请求值映射，例如前缀 gpt 同时将 high、xhigh 转到 medium。转发值可选拒绝，命中对应请求值时直接返回错误。精确优先于前后缀，更长前后缀优先。",
        "addReasoningEffortPair": "添加请求值",
        "removeReasoningEffortPair": "删除请求值",
        "reasoningEffortMatchType": "类型",
        "reasoningEffortModel": "模型",
        "reasoningEffortMatchExact": "精确",
        "reasoningEffortMatchPrefix": "前缀",
        "reasoningEffortMatchSuffix": "后缀",
        "reasoningEffortMatchTypePlaceholder": "全匹配",
        "reasoningEffortModelPlaceholder": "留空则全部 / gpt / gpt-5.4",
        "reasoningEffortToDeny": "拒绝",
        "unsupportedMatchType": "匹配类型仅支持精确、前缀或后缀",
        "duplicateScope": "类型和模型组合不能重复"
      },
      "platforms": {
        "kimi": "Kimi",
        "minimax": "MiniMax"
      },
      "videoPricing": {
        "modelOverridesTitle": "按模型覆盖视频价格",
        "modelOverridesDescription": "已填写的单元格会覆盖该模型族的平面分辨率价格。video-1.5 的 preview 与 legacy 别名共用同一模型族；留空则回退到平面分辨率价格。"
      },
      "explicitPricing": {
        "title": "Grok 搜索与 Voice 定价",
        "description": "分组级 web_search（每千次）与 Voice realtime / TTS / STT 单价（USD）。留空表示未配置。",
        "searchPricePer1k": "搜索每千次价格（USD）",
        "pricePlaceholder": "可选"
      },
      "modelPricing": {
        "title": "分组逐模型定价",
        "description": "匹配模型后覆盖渠道和内置价格。长上下文阶梯沿用官方/预设价卡，无需再手填区间。音频可用按次层级配置 realtime、tts、stt。",
        "longContext": "启用长上下文阶梯定价",
        "longContextHint": "勾选后按渠道区间或官方预设阶梯计费；关闭后默认按第一档，账号显式开启时除外。",
        "add": "添加模型价格"
      },
      "voicePricing": {
        "title": "Grok Voice 定价",
        "description": "分组级 Voice realtime / TTS / STT 单价（USD）。留空表示未配置。",
        "audioRealtimePerMin": "Realtime 每分钟价格（USD）",
        "audioTtsPerMillionChars": "TTS 每百万字符价格（USD）",
        "audioSttPerHour": "STT 每小时价格（USD）",
        "pricePlaceholder": "可选"
      },
      "modelAllowlist": {
        "title": "模型白名单",
        "hint": "开启后，不在白名单中的模型会被拒绝（404 model_not_found），模型列表接口也只展示白名单内的模型。条目支持精确模型 ID 与末尾 * 通配。注意：Claude Code 会用 haiku 系小模型做标题/摘要等探测，/messages/count_tokens 同样受白名单控制，请一并勾选所需的小模型。",
        "loading": "正在加载候选模型...",
        "empty": "暂无候选模型，可在下方手工添加条目",
        "selectedSummary": "已选 {selected} / {total}",
        "selectAll": "全选",
        "invertSelection": "反选",
        "wildcardTag": "通配",
        "customPlaceholder": "自定义条目，如 claude-* 或 gpt-5.5-codex",
        "addCustom": "添加",
        "emptySelectionError": "模型白名单已开启，请至少选择或添加一个模型条目",
        "errors": {
          "empty": "请输入模型条目",
          "invalidWildcard": "通配符 * 只能出现在条目末尾",
          "duplicate": "该条目已存在"
        }
      },
      "codexModelsManifest": {
        "title": "固定账号获取模型列表",
        "hint": "开启后，普通模型列表与 Codex Model Manifest 均优先从选定账号获取并合并，再应用账号映射和分组列表过滤；限流/过载中的选定账号仍会被使用。",
        "enable": "使用特定账号获取模型列表",
        "enabledHint": "账号来源限定为当前分组内的 OpenAI 账号，最多选择 10 个。",
        "disabledHint": "未启用：普通列表使用本地映射或默认模型；Codex 优先使用本地目录，无本地目录时由调度器选账。",
        "accounts": "选定账号",
        "searchPlaceholder": "搜索账号（当前分组内 OpenAI 账号）",
        "searchEmpty": "未找到匹配账号",
        "fallback": "选定账号全部不可用时回退调度器",
        "fallbackHint": "关闭时返回 503 / 上游错误；开启时回退到现有调度器选账路径。",
        "selectAtLeastOne": "开启固定账号后至少选择一个账号"
      },
      "openaiLive": {
        "enableAnyway": "仍然开启"
      },
      "openaiFast": {
        "title": "OpenAI Fast 模式",
        "force": "强制使用 Fast（priority）",
        "hint": "开启后，此分组的 OpenAI 请求会强制写入 service_tier=priority；全局 Fast/Flex 策略仍可过滤或拦截。保存后新请求立即生效，已建立的 WebSocket 会话需重连。",
        "free": "免费 Fast",
        "freeHint": "该分组的 Fast 请求仍使用 priority 档位，但客户实际费用按同一请求的 Standard 价格计算。"
      },
      "claudeMaxSimulation": {
        "title": "Claude Max 用量模拟",
        "tooltip": "启用后，对于没有上游缓存写入用量的 Claude 模型，系统会确定性地将 token 映射为少量输入加 1h 缓存创建，同时保持总 token 不变。",
        "enabled": "已启用（模拟 1h 缓存）",
        "disabled": "已禁用",
        "hint": "仅调整用量计费日志中的 token 类别。不会持久化每个请求的映射状态。"
      }
    },
    "accounts": {
      "platforms": {
        "kimi": "Kimi",
        "minimax": "MiniMax"
      },
      "cnProviders": {
        "accountMode": {
          "title": "账号类型",
          "payg": "按量付费",
          "paygDesc": "消耗账户余额，按 Token 计费。余额不足自动冷却，充值后恢复。",
          "coding": "Coding Plan",
          "codingDesc": "订阅制编程套餐，按 5 小时 / 每周滚动用量窗口限流。"
        },
        "apiProtocol": {
          "title": "API 协议",
          "adaptive": "自适应",
          "adaptiveDesc": "按入站协议优先使用供应商原生端点，仅在没有对应端点时转换。",
          "endpoints": "协议端点",
          "responsesFallbackDesc": "该供应商没有原生 Responses 端点，Responses 请求将转换为 Chat Completions。",
          "chatCompletions": "Chat Completions",
          "chatCompletionsDesc": "标准 OpenAI 兼容端点，其他格式请求将被转换。",
          "anthropic": "Anthropic",
          "anthropicDesc": "直通供应商原生 Anthropic 端点，零转换，适配 Claude Code。",
          "responses": "Responses",
          "responsesDesc": "供应商原生 Responses 端点，适配 Codex。"
        },
        "zhipuTeam": {
          "title": "团队版组织 / 项目 ID",
          "organization": "组织 ID（团队版可选）",
          "organizationPlaceholder": "团队版 Coding Plan 的组织 ID",
          "project": "项目 ID（团队版可选）",
          "projectPlaceholder": "团队版 Coding Plan 的项目 ID",
          "hint": "仅团队版 GLM Coding Plan 需要填写，填写后用量查询走团队版端点；个人版留空即可。获取方式点击左侧问号查看教程。",
          "help": {
            "title": "如何获取组织 / 项目 ID",
            "step1": "用团队版账号登录智谱开放平台（bigmodel.cn），进入「Coding Plan → 团队版 → 我的套餐」页面。",
            "step2": "按 F12 打开浏览器开发者工具，切换到「Network / 网络」标签，然后刷新页面。",
            "step3": "在 Network 的筛选框中输入 /api/biz/v1/organization，点击命中的请求（如 api_keys）。",
            "step4": "请求 URL 中 org- 开头的一段即组织 ID、proj_ 开头的一段即项目 ID（也可在 Request Headers 中查看 bigmodel-organization / bigmodel-project 的值），分别填入上方输入框。",
            "example": "示例：…/organization/org-0610bE2D…/projects/proj_0798F20…/api_keys → org-0610bE2D… 填「组织 ID」，proj_0798F20… 填「项目 ID」"
          }
        },
        "balance": "余额 --",
        "window5h": "5h",
        "windowWeekly": "7d",
        "probe": "查询",
        "probeTooltip": "请求供应商额度端点，查询 5 小时 / 每周滚动窗口用量",
        "balanceProbeTooltip": "请求供应商余额端点，查询账户余额",
        "balanceLow": "余额不足",
        "noBalanceEndpoint": "该平台暂无余额查询接口"
      },
      "accountSchedulingThresholdOverride": "账号自动停调阈值覆盖",
      "accountSchedulingThresholdOverrideHint": "仅对当前账号覆盖平台级自动停调阈值；关闭后使用平台设置。",
      "accountSchedulingThresholdOverrideValue": "账号阈值百分比",
      "accountSchedulingThresholdOverrideDisabledHint": "1-100，达到该用量百分比后临时不可调度；100 表示禁用当前账号自动停调。",
      "status": {
        "expired": "已过期",
        "tempUnschedulableUntil": "预计 {time} 恢复"
      },
      "tempUnschedulable": {
        "multipleErrorTrigger": "{minutes} 分钟内累计 {count} 次匹配错误，达到触发阈值（{threshold}）。",
        "multipleErrorTriggerNoWindow": "累计 {count} 次匹配错误，达到触发阈值（{threshold}）。",
        "multipleErrorCountInWindow": "{minutes} 分钟内累计发生 {count} 次匹配错误。",
        "multipleErrorCount": "本次不可调度由累计 {count} 次匹配错误触发。"
      },
      "usageWindow": {
        "grokUsed": "已用 $",
        "grokBalance": "余额 $",
        "grokPrepaid": "预付余额",
        "grokMonthlyLimit": "月度已用/上限（USD）",
        "grokOverage": "超额 onDemandUsed/onDemandCap",
        "grokOverageShort": "超额 $",
        "estimatedTotalCost": "预计总费用 ${cost}",
        "estimatedTotalCostTooltip": "根据当前窗口费用和使用率估算达到 100% 使用率时的总费用"
      },
      "openaiQuotaReset": {
        "autoStatus": {
          "checking": "检测中",
          "available": "卡可用",
          "resetting": "自动重置中",
          "success": "自动重置成功",
          "noCredit": "无卡",
          "failed": "自动重置失败"
        }
      },
      "bulkEdit": {
        "successWithInherited": "成功更新 {count} 个账号；其中 {inherited} 个影子账号仍跟随母账号。",
        "partialSuccessWithInherited": "部分更新成功：成功 {success} 个，失败 {failed} 个；其中 {inherited} 个影子账号仍跟随母账号。",
        "longContextShadowHint": "长上下文计费归母账号所有。选中的影子账号仍跟随母账号，筛选全量目标时同样如此。",
        "longContextParentRequired": "选中的账号全部是影子账号，请选择母账号修改长上下文计费。"
      },
      "upstreamRequestIdHeader": "上游ID",
      "upstreamRequestIdHeaderPlaceholder": "留空不记录",
      "upstreamRequestIdHeaderHelp": {
        "intro": "填写直接上游在响应头中声明请求标识的头名，记录到用量明细的“上游ID”列；留空则不记录。",
        "examplesTitle": "常见取值",
        "sub2apiNote": "对应对方用量明细的请求ID列",
        "official": "{platform} 官方 API"
      },
      "openai": {
        "imagesUrlToB64Json": "生图结果 URL 转 base64",
        "imagesUrlToB64JsonDesc": "仅对 OpenAI API Key 的 Images 非流式响应生效。上游返回的图片缺少 b64_json 但带 url 时，网关下载该 url 并以 base64 回填 b64_json（url 保留），兼容按官方接口实现的客户端；下载失败则原样返回。",
        "codexFingerprintMode": "Codex 指纹收敛",
        "codexFingerprintModeDesc": "多人共享同一 OAuth 账号时，将各用户的设备/会话标识收敛为账号级恒定值，减少上游可见的设备数和会话数。默认关闭（原样透传客户端标识），需要时再显式开启；部分账号开启收敛后出现过额度缩水，请按自己的实测结果选择。",
        "codexFingerprintOff": "关闭（透传，默认）",
        "codexFingerprintDevice": "仅设备",
        "codexFingerprintSession": "设备+会话",
        "codexFingerprintFull": "完全收敛"
      },
      "grok": {
        "testMode": "测试模式",
        "testModeHint": "文本 / 图片 / 视频使用所选模型。网页搜索、TTS、STT、Realtime 走独立接口探测（不是对话里的 tools）。",
        "testModeText": "文本（Responses）",
        "testModeImage": "图片（/images/generations）",
        "testModeVideo": "视频（/videos/generations）",
        "testModeSearch": "网页搜索（/web_search）",
        "testModeTTS": "语音合成 TTS（/tts）",
        "testModeSTT": "语音识别 STT（/stt）",
        "testModeRealtime": "实时语音 Realtime（WS /realtime）",
        "textTestMode": "模式：文本（Responses）",
        "searchTestMode": "模式：网页搜索（/web_search）",
        "ttsTestMode": "模式：TTS（/tts）",
        "sttTestMode": "模式：STT（/stt）",
        "realtimeTestMode": "模式：Realtime（WS /realtime）",
        "searchQueryLabel": "搜索关键词",
        "searchQueryPlaceholder": "例如：xAI Grok",
        "searchQueryDefault": "xAI Grok",
        "searchTestHint": "独立网页搜索探测（与网关 /v1/web_search 语义一致），不是带 tools 的自由对话。",
        "ttsTextLabel": "TTS 文本",
        "ttsTextPlaceholder": "例如：Hello from Sub2API connectivity test.",
        "ttsTextDefault": "Hello from Sub2API account connectivity test.",
        "ttsTestHint": "独立调用 /v1/tts（language=en）；成功时显示音频字节数。",
        "sttTestHint": "独立调用 /v1/stt，使用合成静音 WAV；成功表示接口可达。",
        "realtimeTestHint": "独立 WebSocket 拨号 /v1/realtime（model=grok-voice-latest）。握手成功即连通；若有首包服务端事件会一并显示。",
        "sendingSearchRequest": "正在发送独立 web_search 请求...",
        "sendingTTSRequest": "正在发送独立 /tts 请求...",
        "sendingSTTRequest": "正在发送独立 /stt 请求...",
        "sendingRealtimeRequest": "正在拨号独立 /realtime WebSocket...",
        "selectedTestMode": "测试模式：{mode}",
        "imageUploadLabel": "源图片（可选，图生图/编辑）",
        "videoFirstFrameLabel": "首帧 / 参考图（可选）",
        "imageUploadHint": "建议 PNG/JPEG，宽高均 ≥ 8 像素，编辑建议小于约 4MB。上传源图会走 /images/edits（图生图）；不上传则走 /images/generations 文生图。",
        "videoFirstFrameHint": "可选首帧/参考图用于图生视频。建议 PNG/JPEG，宽高均 ≥ 8 像素。",
        "audioUploadLabel": "音频文件（STT 可选）",
        "audioUploadHint": "上传真实音频做转写；不上传则用静音 WAV 仅测连通。",
        "mediaTooLarge": "文件过大（管理端测试上传上限约 6MB）。",
        "chooseImageFile": "选择图片",
        "chooseAudioFile": "选择音频",
        "uploadPreviewAlt": "上传预览",
        "fileReadFailed": "读取所选文件失败",
        "noResponseBody": "服务器未返回响应体"
      },
      "syncUpstreamModelsMetadataIncomplete": "模型 ID 已同步，但未能更新任何能力元数据。",
      "syncUpstreamModelsMetadataPartial": "已更新部分模型的能力元数据；其余模型能力仍不完整。",
      "grokMediaEligibility": {
        "title": "媒体生成资格",
        "hint": "控制该 Grok OAuth 账号是否可被图片和视频生成请求选中。",
        "auto": "自动判断",
        "enabled": "强制启用",
        "disabled": "强制禁用",
        "current": "当前判定：",
        "eligible": "可用",
        "ineligible": "不可用",
        "loading": "正在读取媒体资格…",
        "loadFailed": "无法读取媒体资格",
        "autoHint": "自动判断只会清除手工覆盖，不会主动触发媒体请求。",
        "forceEnableWarning": "强制启用会绕过自动资格检查，仅应对已确认支持生图/生视频的账号使用。",
        "partialSave": "账号其他配置可能已保存，但媒体资格未更新，请重试。",
        "reasons": {
          "eligible": "已确认付费资格",
          "billing_inconclusive": "Billing 信息不明确",
          "billing_forbidden": "Billing 接口拒绝访问",
          "billing_free_tier": "Free 账号",
          "billing_unobserved": "尚未探测到 Billing",
          "override_enabled": "手工强制启用",
          "override_disabled": "手工强制禁用"
        }
      },
      "autoResetCredit": {
        "title": "自动使用重置卡",
        "hint": "仅在实际用量达到阈值时使用最早到期的可用卡；默认关闭。无卡或失败时账号保持暂停。",
        "threshold5h": "5h 自动用卡阈值(%)",
        "threshold7d": "7d 自动用卡阈值(%)",
        "thresholdHint": "两个窗口独立判断，任一达到自身阈值即触发。可填写 0.1–100，默认均为 100。",
        "thresholdInvalid": "自动使用重置卡阈值必须在 0.1% 到 100% 之间。"
      },
      "expiresAtTimezoneHint": "输入按浏览器本地时区（{timezone}）解释。",
      "oauth": {
        "grok": {
          "emailPasswordAuth": "邮箱密码登录",
          "emailPasswordDesc": "使用 Grok 网页邮箱与密码登录。服务端仅用密码换取临时 SSO 再转 Build OAuth；密码与 raw SSO 均不会写入账号凭据。",
          "emailPasswordInputLabel": "邮箱----密码",
          "emailPasswordPlaceholder": "user{'@'}example.com----your-password\n支持多个，每行一组",
          "emailPasswordHint": "格式：email----password（密码可含 -）。需要配置 YesCaptcha 密钥；建议搭配代理。",
          "pleaseEnterPassword": "请输入 email----password（每行一组）",
          "pleaseEnterSSOToken": "请输入 SSO Token",
          "failedToValidateSSO": "校验 Grok SSO 失败",
          "failedToAuthorizePassword": "Grok 密码授权失败"
        }
      },
      "errorPrefix": "错误：{message}",
      "imagePreviewAlt": "测试图片 {index}",
      "imageLightboxAlt": "图片预览",
      "videoTestMode": "模式：视频生成测试",
      "videoPreview": "生成视频：",
      "videoReceived": "已收到第 {count} 段测试视频"
    },
    "redeem": {
      "expiryDateRequired": "请输入有效的过期日期和时间",
      "localTimeZoneHint": "自定义时间按浏览器本地时区（{timezone}）解释。"
    },
    "announcements": {
      "createFirstAnnouncement": "还没有公告，创建您的第一条公告。"
    },
    "usage": {
      "upstreamRequestId": "上游ID",
      "upstreamRequestIdCopied": "上游ID已复制"
    }
  }
}
