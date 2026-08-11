<template>
  <v-container>
    <header class="mb-5">
      <h1 class="text-h4">{{ $t('security.title') }}</h1>
      <p class="text-body-1">{{ $t('security.subtitle') }}</p>
    </header>

    <v-alert v-if="errorMessage" class="mb-4" role="alert" type="error" variant="tonal">
      {{ errorMessage }}
    </v-alert>
    <v-progress-linear v-if="loading" indeterminate :aria-label="$t('security.loading')" />

    <template v-else-if="posture">
      <v-card class="mb-5">
        <v-card-title>{{ $t('security.passwordTitle') }}</v-card-title>
        <v-card-subtitle>{{ $t('security.passwordDescription') }}</v-card-subtitle>
        <v-card-text>
          <v-alert
            class="mb-4"
            :type="posture.passwordPolicyVersion >= posture.passwordPolicyCurrentVersion ? 'success' : 'warning'"
            variant="tonal"
          >
            {{
              posture.passwordPolicyVersion >= posture.passwordPolicyCurrentVersion
                ? $t('security.passwordPolicyCurrent')
                : $t('security.passwordPolicyLegacy')
            }}
          </v-alert>
          <v-row>
            <v-col cols="12" md="3">
              <v-select
                v-if="posture.mfa.enabled"
                v-model="passwordAssuranceMethod"
                :items="assuranceMethods"
                item-title="title"
                item-value="value"
                :label="$t('security.assuranceMethod')"
              />
              <v-text-field
                v-model="passwordCredential"
                :autocomplete="posture.mfa.enabled ? 'one-time-code' : 'current-password'"
                :label="passwordCredentialLabel"
                :type="posture.mfa.enabled ? 'text' : 'password'"
              />
            </v-col>
            <v-col cols="12" md="3">
              <v-text-field
                v-model="newUsername"
                autocomplete="username"
                :label="$t('security.newUsername')"
              />
            </v-col>
            <v-col cols="12" md="3">
              <v-text-field
                v-model="newPassword"
                autocomplete="new-password"
                :hint="$t('security.passwordHint')"
                :label="$t('security.newPassword')"
                persistent-hint
                type="password"
              />
            </v-col>
            <v-col cols="12" md="3">
              <v-text-field
                v-model="confirmPassword"
                autocomplete="new-password"
                :label="$t('security.confirmPassword')"
                type="password"
              />
            </v-col>
          </v-row>
          <v-btn color="primary" :loading="working" @click="submitPasswordChange">
            {{ $t('security.changePassword') }}
          </v-btn>
        </v-card-text>
      </v-card>

      <v-card class="mb-5">
        <v-card-title>{{ $t('security.connectionPostureTitle') }}</v-card-title>
        <v-card-subtitle>{{ $t('security.connectionPostureDescription') }}</v-card-subtitle>
        <v-card-text>
          <v-row>
            <v-col cols="12" sm="6" lg="3">
              <div class="text-caption">{{ $t('security.clientIdentitySource') }}</div>
              <div>{{ posture.clientIdentity.provenance }}</div>
            </v-col>
            <v-col cols="12" sm="6" lg="3">
              <div class="text-caption">{{ $t('security.schemePosture') }}</div>
              <div>
                {{ posture.clientIdentity.actualScheme }} -> {{ posture.clientIdentity.desiredScheme }}
                ({{ posture.clientIdentity.schemeSource }})
              </div>
            </v-col>
            <v-col cols="12" sm="6" lg="3">
              <div class="text-caption">{{ $t('security.trustedProxyPosture') }}</div>
              <div>
                {{
                  $t('security.trustedProxySummary', {
                    configured: posture.clientIdentity.trustedProxyCount,
                    traversed: posture.clientIdentity.trustedProxyHops,
                  })
                }}
              </div>
              <div class="text-caption">
                {{ $t('security.trustedProxySource', { source: posture.clientIdentity.trustedProxySource }) }}
              </div>
            </v-col>
            <v-col cols="12" sm="6" lg="3">
              <div class="text-caption">{{ $t('security.cookiePosture') }}</div>
              <div>
                Secure={{ posture.cookiePolicy.secure }} | HttpOnly={{ posture.cookiePolicy.httpOnly }}
                | SameSite={{ posture.cookiePolicy.sameSite }}
              </div>
            </v-col>
          </v-row>
          <v-alert
            v-if="!posture.clientIdentity.forwardedValid"
            class="mt-3"
            type="warning"
            variant="tonal"
          >
            {{ $t('security.forwardedInvalid') }}
          </v-alert>
          <v-alert
            v-if="posture.clientIdentity.warningCodes.length"
            class="mt-3"
            type="warning"
            variant="tonal"
          >
            {{ $t('security.trustedProxyWarning', { codes: posture.clientIdentity.warningCodes.join(', ') }) }}
          </v-alert>
          <p class="mt-3 text-caption">
            {{ $t('security.trustedProxyActual', {
              value: posture.clientIdentity.trustedProxyCidrs.join(', ') || $t('security.noneConfigured'),
            }) }}
          </p>
        </v-card-text>
      </v-card>

      <v-row>
        <v-col cols="12" lg="5">
          <v-card>
            <v-card-title>{{ $t('security.mfaTitle') }}</v-card-title>
            <v-card-text>
              <v-alert class="mb-3" type="warning" variant="tonal">
                {{ $t('security.totpPhishingWarning') }}
              </v-alert>
              <v-chip :color="posture.mfa.enabled ? 'success' : 'warning'" class="mb-3">
                {{ posture.mfa.enabled ? $t('security.enabled') : $t('security.disabled') }}
              </v-chip>
              <p v-if="posture.mfa.enabled" class="mb-3">
                {{ $t('security.recoveryRemaining', { count: posture.mfa.recoveryRemaining }) }}
              </p>

              <template v-if="enrollment">
                <section aria-labelledby="mfa-enrollment-heading">
                  <h2 id="mfa-enrollment-heading" class="text-h6">{{ $t('security.scanQR') }}</h2>
                  <qrcode-vue
                    :aria-label="$t('security.qrAlt')"
                    class="my-3"
                    :size="192"
                    :value="enrollment.uri"
                  />
                  <v-text-field
                    :label="$t('security.manualSecret')"
                    :model-value="enrollment.secret"
                    readonly
                  />
                  <v-text-field
                    v-model="confirmationCode"
                    autocomplete="one-time-code"
                    :label="$t('security.authenticatorCode')"
                    inputmode="numeric"
                  />
                  <v-btn color="primary" :loading="working" @click="confirmEnrollment">
                    {{ $t('security.confirmEnrollment') }}
                  </v-btn>
                </section>
              </template>

              <template v-else-if="recoveryCodes.length">
                <section aria-labelledby="recovery-codes-heading">
                  <h2 id="recovery-codes-heading" class="text-h6">{{ $t('security.recoveryCodesTitle') }}</h2>
                  <v-alert class="my-3" type="warning" variant="tonal">
                    {{ $t('security.recoveryCodesWarning') }}
                  </v-alert>
                  <ol class="recovery-codes" aria-live="polite">
                    <li v-for="code in recoveryCodes" :key="code"><code>{{ code }}</code></li>
                  </ol>
                  <v-btn class="mt-3" variant="outlined" @click="copyRecoveryCodes">
                    {{ $t('security.copyRecoveryCodes') }}
                  </v-btn>
                  <v-btn class="mt-3 ml-2" color="primary" :loading="working" @click="ackRecoveryCodes">
                    {{ $t('security.savedRecoveryCodes') }}
                  </v-btn>
                </section>
              </template>

              <template v-else>
                <v-select
                  v-if="posture.mfa.enabled"
                  v-model="assuranceMethod"
                  :items="assuranceMethods"
                  item-title="title"
                  item-value="value"
                  :label="$t('security.assuranceMethod')"
                />
                <v-text-field
                  v-model="credential"
                  :autocomplete="posture.mfa.enabled ? 'one-time-code' : 'current-password'"
                  :label="credentialLabel"
                  :type="posture.mfa.enabled ? 'text' : 'password'"
                />
                <v-btn color="primary" :loading="working" @click="startEnrollment">
                  {{ posture.mfa.enabled ? $t('security.replaceAuthenticator') : $t('security.enableMFA') }}
                </v-btn>
                <template v-if="posture.mfa.enabled">
                  <v-btn class="ml-2" variant="outlined" :loading="working" @click="rotateCodes">
                    {{ $t('security.rotateRecoveryCodes') }}
                  </v-btn>
                  <v-btn class="ml-2" color="error" variant="outlined" :loading="working" @click="turnOffMFA">
                    {{ $t('security.disableMFA') }}
                  </v-btn>
                </template>
              </template>
            </v-card-text>
          </v-card>
        </v-col>

        <v-col cols="12" lg="7">
          <v-card>
            <v-card-title>{{ $t('security.sessionsTitle') }}</v-card-title>
            <v-card-subtitle>{{ $t('security.sessionsDescription') }}</v-card-subtitle>
            <v-card-text>
              <v-alert
                v-if="posture.sessionLifetimePolicy !== 'bounded_v1'"
                class="mb-4"
                type="warning"
                variant="tonal"
              >
                <div class="font-weight-medium">{{ $t('security.legacySessionPolicyTitle') }}</div>
                <div>{{ $t('security.legacySessionPolicyDescription') }}</div>
              </v-alert>
              <v-list lines="three">
                <v-list-item v-for="session in sessions" :key="session.ref">
                  <template #prepend>
                    <v-icon :icon="session.current ? 'mdi-monitor-lock' : 'mdi-monitor'" />
                  </template>
                  <v-list-item-title>
                    {{ session.deviceLabel || $t('security.unknownDevice') }}
                    <v-chip v-if="session.current" class="ml-2" size="small">{{ $t('security.currentSession') }}</v-chip>
                  </v-list-item-title>
                  <v-list-item-subtitle>
                    {{ session.clientPrefix }} · {{ formatDate(session.lastSeenAt) }} · {{ session.assurance }}
                    · {{ lifetimeLabel(session.lifetimePosture) }}
                    <template v-if="session.lastMfaAt">
                      · {{ $t('security.lastMfaAt', { value: formatDate(session.lastMfaAt) }) }}
                    </template>
                  </v-list-item-subtitle>
                  <template #append>
                    <v-btn
                      :aria-label="$t('security.revokeSession')"
                      color="error"
                      icon="mdi-logout-variant"
                      variant="text"
                      @click="revokeOne(session)"
                    />
                  </template>
                </v-list-item>
              </v-list>
              <v-divider class="my-3" />
              <p class="mb-2">{{ $t('security.revokeOthersDescription') }}</p>
              <v-text-field
                v-model="credential"
                :autocomplete="posture.mfa.enabled ? 'one-time-code' : 'current-password'"
                :label="credentialLabel"
                :type="posture.mfa.enabled ? 'text' : 'password'"
              />
              <v-btn color="error" variant="outlined" :loading="working" @click="revokeOthers">
                {{ $t('security.revokeOtherSessions') }}
              </v-btn>
              <v-btn
                v-if="posture.sessionLifetimePolicy !== 'bounded_v1'"
                class="ml-2"
                color="primary"
                variant="outlined"
                :loading="working"
                @click="adoptBounded"
              >
                {{ $t('security.adoptBoundedSessions') }}
              </v-btn>
            </v-card-text>
          </v-card>
        </v-col>
      </v-row>
    </template>
  </v-container>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRouter } from 'vue-router'
import QrcodeVue from 'qrcode.vue'
import {
  acknowledgeRecoveryCodes,
  adoptBoundedSessions,
  beginMFAEnrollment,
  changePassword,
  confirmMFAEnrollment,
  disableMFA,
  getSecurityPosture,
  getSecuritySessions,
  issueStepUp,
  messageObject,
  revokeOtherSessions,
  revokeSession,
  rotateRecoveryCodes,
  type SecurityPosture,
  type SecuritySession,
  type SessionInventory,
} from '@/shared/composables/useSecurityOperations'

const { t } = useI18n()
const router = useRouter()
const posture = ref<SecurityPosture | null>(null)
const sessions = ref<SecuritySession[]>([])
const loading = ref(true)
const working = ref(false)
const errorMessage = ref('')
const credential = ref('')
const assuranceMethod = ref<'password' | 'totp' | 'recovery'>('password')
const passwordAssuranceMethod = ref<'password' | 'totp' | 'recovery'>('password')
const passwordCredential = ref('')
const newUsername = ref('')
const newPassword = ref('')
const confirmPassword = ref('')
const enrollment = ref<{ secret: string; uri: string; expiresAt: number } | null>(null)
const confirmationCode = ref('')
const recoveryCodes = ref<string[]>([])

const assuranceMethods = computed(() => posture.value?.mfa.enabled
  ? [
      { title: t('security.authenticatorCode'), value: 'totp' },
      { title: t('security.recoveryCode'), value: 'recovery' },
    ]
  : [{ title: t('security.currentPassword'), value: 'password' }])

const credentialLabel = computed(() => {
  if (!posture.value?.mfa.enabled) return t('security.currentPassword')
  return assuranceMethod.value === 'recovery' ? t('security.recoveryCode') : t('security.authenticatorCode')
})

const passwordCredentialLabel = computed(() => {
  if (!posture.value?.mfa.enabled) return t('security.currentPassword')
  return passwordAssuranceMethod.value === 'recovery' ? t('security.recoveryCode') : t('security.authenticatorCode')
})

const load = async () => {
  loading.value = true
  const [postureResponse, sessionResponse] = await Promise.all([
    getSecurityPosture(),
    getSecuritySessions(),
  ])
  posture.value = messageObject<SecurityPosture>(postureResponse)
  sessions.value = messageObject<SessionInventory>(sessionResponse)?.items ?? []
  assuranceMethod.value = posture.value?.mfa.enabled ? 'totp' : 'password'
  passwordAssuranceMethod.value = posture.value?.mfa.enabled ? 'totp' : 'password'
  if (posture.value && !newUsername.value) newUsername.value = posture.value.username
  loading.value = false
}

onMounted(load)

const withStepUp = async (operationKind: string, targetDigest: string, action: (token: string) => Promise<any>) => {
  errorMessage.value = ''
  working.value = true
  try {
    const method = posture.value?.mfa.enabled ? assuranceMethod.value : 'password'
    const grantResponse = await issueStepUp({
      method,
      credential: credential.value,
      operationKind,
      targetDigest,
    })
    credential.value = ''
    const grant = messageObject<{ token: string }>(grantResponse)
    if (!grant) {
      errorMessage.value = grantResponse.msg
      return
    }
    const response = await action(grant.token)
    if (!response.success) {
      errorMessage.value = response.msg
    }
  } finally {
    credential.value = ''
    working.value = false
  }
}

const startEnrollment = async () => {
  if (!posture.value) return
  await withStepUp('mfa.enroll', posture.value.stepUpTargets.self, async (token) => {
    const response = await beginMFAEnrollment(token)
    enrollment.value = messageObject(response)
    return response
  })
}

const submitPasswordChange = async () => {
  if (!posture.value) return
  errorMessage.value = ''
  if (newPassword.value !== confirmPassword.value) {
    errorMessage.value = t('security.passwordMismatch')
    return
  }
  working.value = true
  try {
    const grantResponse = await issueStepUp({
      method: posture.value.mfa.enabled ? passwordAssuranceMethod.value : 'password',
      credential: passwordCredential.value,
      operationKind: 'admin.credential',
      targetDigest: posture.value.stepUpTargets.self,
    })
    passwordCredential.value = ''
    const grant = messageObject<{ token: string }>(grantResponse)
    if (!grant) {
      errorMessage.value = grantResponse.msg
      return
    }
    const response = await changePassword({
      newUsername: newUsername.value,
      newPassword: newPassword.value,
      stepUpToken: grant.token,
    })
    newPassword.value = ''
    confirmPassword.value = ''
    if (!response.success) {
      errorMessage.value = response.msg
      return
    }
    await load()
  } finally {
    passwordCredential.value = ''
    newPassword.value = ''
    confirmPassword.value = ''
    working.value = false
  }
}

const confirmEnrollment = async () => {
  working.value = true
  try {
    const response = await confirmMFAEnrollment(confirmationCode.value)
    confirmationCode.value = ''
    const result = messageObject<{ recoveryCodes: string[] }>(response)
    if (!result) {
      errorMessage.value = response.msg
      return
    }
    enrollment.value = null
    recoveryCodes.value = result.recoveryCodes
  } finally {
    working.value = false
  }
}

const copyRecoveryCodes = async () => {
  await navigator.clipboard.writeText(recoveryCodes.value.join('\n'))
}

const ackRecoveryCodes = async () => {
  working.value = true
  const response = await acknowledgeRecoveryCodes()
  working.value = false
  if (!response.success) {
    errorMessage.value = response.msg
    return
  }
  recoveryCodes.value = []
  await load()
}

const rotateCodes = async () => {
  if (!posture.value) return
  await withStepUp('mfa.recovery.rotate', posture.value.stepUpTargets.self, async (token) => {
    const response = await rotateRecoveryCodes(token)
    const result = messageObject<{ recoveryCodes: string[] }>(response)
    if (result) recoveryCodes.value = result.recoveryCodes
    return response
  })
}

const turnOffMFA = async () => {
  if (!posture.value) return
  await withStepUp('mfa.disable', posture.value.stepUpTargets.self, async (token) => {
    const response = await disableMFA(token)
    if (response.success) await load()
    return response
  })
}

const revokeOne = async (session: SecuritySession) => {
  const response = await revokeSession(session.ref)
  if (!response.success) {
    errorMessage.value = response.msg
    return
  }
  if (session.current) {
    await router.replace('/login')
    return
  }
  await load()
}

const revokeOthers = async () => {
  if (!posture.value) return
  await withStepUp('sessions.revoke_others', posture.value.stepUpTargets.revokeOthers, async (token) => {
    const response = await revokeOtherSessions(token)
    if (response.success) await load()
    return response
  })
}

const adoptBounded = async () => {
  if (!posture.value) return
  await withStepUp('sessions.adopt_bounded', posture.value.stepUpTargets.adoptBounded, async (token) => {
    const response = await adoptBoundedSessions(token)
    if (response.success) await load()
    return response
  })
}

const lifetimeLabel = (posture: string) => {
  switch (posture) {
    case 'bounded_v1':
      return t('security.sessionLifetimeBounded')
    case 'legacy_unbounded':
      return t('security.sessionLifetimeLegacyUnbounded')
    case 'legacy_explicit':
      return t('security.sessionLifetimeLegacyExplicit')
    default:
      return posture
  }
}

const formatDate = (unix: number) => new Intl.DateTimeFormat(undefined, {
  dateStyle: 'medium',
  timeStyle: 'short',
}).format(new Date(unix * 1000))
</script>

<style scoped>
.recovery-codes {
  columns: 2;
  font-size: 1rem;
  line-height: 1.8;
}

@media (max-width: 600px) {
  .recovery-codes {
    columns: 1;
  }
}
</style>
