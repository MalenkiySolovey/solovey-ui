export default { panelUpdateComponent: {
  lifecycleResult: '生命週期結果', lifecycle: { STARTED: '啟動已完成', START_FAILED: '啟動失敗', STOPPED: '停止已完成', STOP_FAILED: '停止失敗', NOT_OBSERVED: '尚未觀察到結果', BINARY_ABSENT: '此建置中缺少程式碼', NOT_INSTALLED: '未安裝' },
  panelVersion: '面板版本', componentCatalog: '元件目錄', binaryProfile: '建置設定檔', unknown: '未知', authority: '狀態來源', runningBinary: '目前執行的建置',
  installedComponents: '已安裝元件', noneInstalled: '沒有已安裝元件。', enabled: '已啟用', disabled: '已停用', locked: '已鎖定', enable: '啟用', disable: '停用', remove: '移除',
  availableForBuild: '此建置中可用', noneAvailable: '沒有等待安裝的內建元件。', install: '安裝', unavailable: '無法使用', unavailableForProfile: '此設定檔中無法使用', noneUnavailable: '未偵測到無法使用的元件。', notBundled: '未內建', requiresNewerPanel: '需要更新的面板',
  removeTitle: '移除元件', removeWarning: '請先停用元件。保留的資料、資源或未完成的復原會阻止移除。', removeConfirm: '從設定檔移除 {name}？此操作不會清理資料。需要時請使用擁有者獨立的「刪除資料」流程。', password: '密碼',
  current: '目前', latest: '最新', requiresPanel: '需要面板', incompatible: '不相容', releaseOnly: '僅隨發行提供',
} }
