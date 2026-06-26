import HttpUtils, { type Msg } from '@/plugins/httputil'

export const restoreDatabase = (form: FormData): Promise<Msg> => HttpUtils.post('api/importdb', form)
export const loadBackupSettings = (): Promise<Msg> => HttpUtils.get('api/settings')
export const importXuiDatabase = (form: FormData): Promise<Msg> => HttpUtils.post('api/import-xui', form)
