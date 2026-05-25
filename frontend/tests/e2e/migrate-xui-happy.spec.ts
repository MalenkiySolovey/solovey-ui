import { test } from '@playwright/test'

// XFAIL: пункты 43, 44, 45, 46 реестра; полный happy path требует test-db/x-ui.db и test-db/s-ui.db.
test.fixme('upload synthetic db, build plan, apply, download JSON/Markdown report, and rollback', async () => {})

// XFAIL: пункт 45 реестра; generated admin password должен быть скрыт до явного reveal.
test.fixme('generated admin password is shown once via reveal pattern, not raw JSON in DOM', async () => {})

// XFAIL: пункт 46 реестра; reset_required пока не имеет backend force-reset semantics.
test.fixme('adminMode reset_required is disabled or warns until backend contract exists', async () => {})
