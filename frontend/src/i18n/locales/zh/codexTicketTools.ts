export default {
  codexTicketAlerts: {
    title: '打票凭据提醒', enable: '打票提醒', enabled: '打票提醒已开启',
    scopeHint: '仅提醒当前列表中已观察到的凭据失效变化；请保持账号页打开，并开启自动刷新。',
    desktopHint: '同时发送桌面与面板提醒；点击关闭。',
    panelHint: '开启后保留面板提醒；桌面通知需要浏览器支持并授权。',
    message: '{count} 个账号模型的凭据已不可用（{models}），请检查票据状态。'
  },
  codexTicketDiagnostics: {
    invalidationSignals: '撤销时的上游信号',
    notRecorded: '未记录', yes: '是', no: '否',
    response_header_ms: '响应头耗时（毫秒）', peer_addr: '连接对端', http_version: 'HTTP 版本', final_origin: '最终 origin',
    peerHint: '连接对端可能是代理或 CDN，不代表公网出口 IP。',
    first_model: '首声明模型', first_event: '首声明事件', terminal_model: '终态模型', terminal_event: '终态事件', conflict: '模型声明冲突',
    truncated: '模型声明展示已截断；不影响原验收判断。',
    wire_http_status: '传输 HTTP 状态', effective_status: '有效错误状态', type: '错误类型', code: '错误代码', scope: '错误范围', retry_at: '可重试时间', classification_source: '分类依据',
    safety_buffering_enabled: 'Safety Buffering', faster_model: 'Faster Model', active_limit: '当前限制', plan_type: '订阅计划',
    used_percent: '已用比例（%）', reset_at: '重置时间', reset_after_seconds: '重置等待（秒）', window_minutes: '窗口长度（分钟）', limit_reached: '已达限制',
    signalsHint: '上游信号仅用于诊断，不单独证明模型降级或代理故障。'
  },
  codexTicketRuntime: {
    title: '当前采票状态', snapshot: '当次保护状态', loading: '正在读取状态…', loadFailed: '状态读取失败，可刷新重试。',
    rule_matched: '命中拒收静默规则', silence_until: '采集代理静默截止', policy_version: '保护规则版本', harvest_half_open: '本次采集曾为半开', harvest_accepted: '采集候选通过',
    state: '状态', reason: '原因', attempts: '账号已用 / 总尝试', round: '遍历轮数 / 上限', model: '模型', proxy: '代理 ID',
    halfOpen: '半开采集', retryAt: '最早可试探时间', cooldownUntil: '账号冷却截止', generation: '周期', yes: '是', no: '否',
    retryHint: '该时间仅表示最早可重新评估或竞争试探，并不保证代理届时恢复。',
    states: {
      protection_draining: '保护正在排空', lease_expired: '在途租约已到期', stale_completion: '已忽略旧尝试结果',
      half_open_busy: '已有半开采集', reservation_expired: '预留已到期', controls_changed: '配置已变更',
      attempt_not_started: '采集尚未开始', account_retry: '账号等待上游冷却',
      business_active: '业务请求进行中，采票暂停', shared_state_unavailable: '共享状态不可用',
      rejection_retry: '规则拒收后等待重试', rejection_cooldown: '连续规则拒收达到上限，正在冷却',
      idle: '空闲', disabled: '保护未启用', available: '可采集', waiting: '等待准入', reserved: '已预留', running: '正在采集',
      half_open: '半开试探', cooldown: '账号冷却', account_cooldown: '账号冷却', account_busy: '账号有在途采票',
      all_proxies_silent: '全部代理静默', proxy_silent: '代理静默', proxy_half_open: '代理已有半开采集', capacity: '等待代理容量',
      ip_cooling: '出口 IP 冷却', ip_disabled: '出口 IP 已禁用',
      verification_deferred: '业务复验暂缓', budget_exhausted: '账号预算耗尽', unavailable: '共享状态不可用',
      recovery_waiting_for_harvest: '等待有资格的采集恢复代理', model_backoff: '模型失败退避', retry_after: '上游要求等待'
    }
  },
  codexTicketPreview: {
    title: '请求头构造预览', hint: '仅预览应用层请求头。不会发送上游请求、刷新凭据、分配代理、消耗预算或保存应用这些修改。认证使用占位值；请求头名称和值由后端统一校验。',
    model: '目标模型', set: '临时设置请求头', remove: '临时移除请求头', name: '请求头名称', value: '请求头值', add: '新增一项', delete: '移除该项',
    submit: '生成预览', loading: '正在生成…', failed: '预览失败，请重试。', invalidResponse: '返回结果不是有效的未发送预览。',
    notSent: '未发送请求；修改未保存或应用。', before_headers: '修改前', after_headers: '修改后', changes: '差异', noChanges: '没有请求头差异。', sources: '请求头构造来源'
  },
  codexTicketOutcome: {
    success: '出票成功', ticket_rejected: '票据被规则拒收', model_mismatch: '目标模型不匹配', model_failed: '模型响应失败',
    upstream_error: '上游错误', verification_failed: '业务复验失败', verification_deferred: '已采集，复验暂缓',
    canceled: '已取消', controls_changed: '配置变更中止', failed: '未成功出票'
  },
  codexTicketNodes: {
    menu: '节点采集', title: '节点采集', refresh: '刷新', copyJson: '复制全部 JSON', copied: '已复制节点采集 JSON',
    rawWarning: '测试阶段：以下 Cookie、STATE、Authorization 与报文均为原样展示，请勿截图外传。探测结果只随本次响应返回，不写入票池、采集历史或质量记录。',
    loading: '正在读取节点信息…', failed: '节点采集失败', overview: '账号与出口',
    proxyUnavailable: '代理不可用', notConfigured: '未在打票模型中配置', noSlots: '当前没有票据', businessSelected: '业务当前选中',
    usable: '可用', unusable: '不可用', crossRegion: '跨区节点', pending: '待定 Cookie',
    connections: 'WS 连接池', noConnections: '当前没有 WS 连接', handshakeHeaders: '握手响应头', truncated: '已截断',
    sentCookies: '发送的 Cookie', receivedCookies: '返回的 Cookie（Set-Cookie）', noCookies: '无 Cookie',
    excludedByMode: '当前 Cookie 模式不会发送', decodeError: '解析失败',
    probeTitle: '节点探测', probeHint: '按所选来源实际向上游发送探测请求，逐轮记录发送/返回的 __oailb 节点与耗时，用于判断节点是否被锁定。',
    runProbe: '开始探测', probing: '探测中…', businessSlot: '业务当前票据', roundDetail: '第 {index} 轮详情',
    request: '原始请求', response: '原始响应', notRecorded: '未记录', rawJson: '原始 JSON',
    fields: {
      accountStatus: '账号状态', cookieMode: 'Cookie 模式', proxy: '当前出口', egressCountry: '出口国家/大区', ticketEnabled: '打票 / 采集 / 随机代理',
      credentialMode: '凭据/会话/刷新', pool: '票池', strategy: '请求策略', routeAffinity: '路由亲和', serverTime: '服务器时间',
      model: '模型', source: '来源', slot: '票据槽位', count: '轮数（1–5）', slotNode: '票据节点', duration: '总耗时', startedAt: '开始时间'
    },
    sources: { ticket: '重放票据 Cookie', sticky_jar: '空 Cookie 起步并沿用', empty_jar: '每轮空 Cookie' },
    sourceHints: {
      ticket: '使用所选票据的会话和 Cookie（按当前 Cookie 模式过滤），观察节点是否保持。',
      sticky_jar: '第一轮不带 Cookie，之后沿用上一轮返回的 Cookie，观察上游是否把同一会话钉在同一节点。',
      empty_jar: '每轮都用新会话且不带 Cookie，观察上游自然分配的节点分布。'
    },
    rounds: { index: '轮', status: '状态', http: 'HTTP', latency: '头 / 首字节 / 首 delta / 总（ms）', node: '发送 → 返回节点', outcome: '节点变化', effective: '实际节点' },
    outcomes: {
      no_route: '无节点 Cookie', assigned: '新分配', kept: '未返回（沿用）', cleared: '被清除', unparsed: '无法解析',
      refreshed: '同节点续期', changed: '节点变更', no_response: '无响应', skipped: '已跳过'
    }
  }
}
