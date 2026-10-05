export default {
  codexTicketVault: {
    menu: '票库', title: '账号票库', refresh: '刷新', loading: '正在读取票库…', failed: '读取票库失败',
    safeHint: '票库只展示票据元数据（状态、时间、长度、Cookie 名称与单向指纹），不展示也不返回原始 STATE、Cookie 值或 token。账号总容量按模型分配。',
    overview: '票库概览', empty: '当前没有任何模型的票据', noSlots: '该模型当前没有存票', notConfigured: '未在打票模型中配置',
    businessSelected: '业务下一张', verified: '已验证', verificationSkipped: '跳过验证', unverified: '未验证',
    revoke: '作废', revokeAll: '全部作废', revoking: '作废中…', confirm: '确认作废', cancel: '取消',
    revokeAllConfirm: '确认作废该模型全部未作废的票？作废后业务不再使用这些票，库存由采集重新补足。',
    revoked: '已作废 {count} 张票', revokeNoop: '票据已处于作废状态', revokeFailed: '作废失败',
    revokeRemaining: '仍有 {count} 张票未能写入作废标记（本机已停用），请稍后重试。',
    seconds: '{count} 秒',
    summary: { total: '存票', available: '可用', maturing: '沉淀中' },
    usageModes: { immediate: '主票 + 备用', latest_only: '仅最新票', aged: '使用沉淀后的票' },
    fields: {
      ticketEnabled: '账号打票', harvestEnabled: '账号采集', configEnabled: '全局打票', proxy: '代理', credentialMode: '凭据模式',
      poolCapacity: '旧版每模型容量', accountPoolCapacity: '账号总容量', ttl: '票据 TTL', cookieTtl: 'Cookie 保留时间', usageMode: '取票机制', minAge: '最小票龄', historicalValidity: '历史票有效期',
      consumeAfterUse: '用后即删', failClosed: '无票拒绝', serverTime: '服务器时间'
    },
    values: { on: '开启', off: '关闭', available: '可用', unavailable: '不可用' },
    columns: {
      slot: '票位', status: '状态', fingerprint: '指纹', credential: '凭据', node: '节点', age: '票龄', matureAt: '可用时间',
      expiresAt: '过期时间', capturedAt: '采集时间', harvest: '采集出口', invalidation: '作废信息', actions: '操作'
    },
    credentialState: 'STATE 长度 {length}', credentialCookies: 'Cookie：{names}', remaining: '剩余 {count} 秒', origin: '首次采集 {time}',
    statuses: {
      available: '可用', not_selected: '仅最新模式未选中', maturing: '沉淀中', revoked: '已作废', consumed: '已使用', binding: '绑定不匹配', credential: '凭据模式不匹配',
      cookie_missing: '缺少 Cookie', expired: '已过期', unverified: '未验证', unavailable: '不可用'
    },
    reasons: { admin_revoked: '管理员在票库作废' },
    sources: { ticket_vault: '账号票库' }
  }
}
