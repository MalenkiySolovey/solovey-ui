export default { panelUpdateComponent: {
  lifecycleResult: '生命周期结果', lifecycle: { STARTED: '启动已完成', START_FAILED: '启动失败', STOPPED: '停止已完成', STOP_FAILED: '停止失败', NOT_OBSERVED: '尚未观察到结果', BINARY_ABSENT: '此构建中缺少代码', NOT_INSTALLED: '未安装' },
  panelVersion: '面板版本', componentCatalog: '组件目录', binaryProfile: '构建配置', unknown: '未知', authority: '状态来源', runningBinary: '当前运行的构建',
  installedComponents: '已安装组件', noneInstalled: '没有已安装组件。', enabled: '已启用', disabled: '已禁用', locked: '已锁定', enable: '启用', disable: '禁用', remove: '移除',
  availableForBuild: '此构建中可用', noneAvailable: '没有等待安装的内置组件。', install: '安装', unavailable: '不可用', unavailableForProfile: '此配置中不可用', noneUnavailable: '未检测到不可用组件。', notBundled: '未内置', requiresNewerPanel: '需要更新的面板',
  removeTitle: '移除组件', removeWarning: '请先禁用组件。保留的数据、资源或未完成的恢复会阻止移除。', removeConfirm: '从配置中移除 {name}？此操作不会清理数据。需要时请使用所有者独立的“删除数据”流程。', password: '密码',
  current: '当前', latest: '最新', requiresPanel: '需要面板', incompatible: '不兼容', releaseOnly: '仅随发布提供',
} }
