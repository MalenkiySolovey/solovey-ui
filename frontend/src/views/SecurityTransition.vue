<template>
  <v-container class="fill-height security-transition">
    <v-row justify="center" align="center">
      <v-col cols="12" sm="9" md="6">
        <v-card>
          <v-card-title>{{ $t('security.transitionTitle') }}</v-card-title>
          <v-card-subtitle>{{ transitionDescription }}</v-card-subtitle>
          <v-card-text>
            <v-progress-linear v-if="loading" indeterminate aria-label="Loading security posture" />

            <v-form
              v-else-if="posture?.authState === 'password_reset' || posture?.authState === 'mfa_recovery'"
              aria-labelledby="password-transition-heading"
              @submit.prevent="submitPasswordTransition"
            >
              <h2 id="password-transition-heading" class="text-h6 mb-3">
                {{ posture.authState === 'mfa_recovery'
                  ? $t('security.recoveryTransition')
                  : $t('security.passwordTransition') }}
              </h2>
              <v-text-field
                v-if="posture.authState === 'password_reset'"
                v-model="currentPassword"
                :label="$t('security.currentPassword')"
                autocomplete="current-password"
                type="password"
                required
              />
              <v-text-field
                v-model="newUsername"
                :label="$t('security.newUsername')"
                autocomplete="username"
                required
              />
              <v-text-field
                v-model="newPassword"
                :hint="$t('security.passwordHint')"
                :label="$t('security.newPassword')"
                autocomplete="new-password"
                persistent-hint
                type="password"
                required
              />
              <v-text-field
                v-model="confirmPassword"
                :label="$t('security.confirmPassword')"
                autocomplete="new-password"
                type="password"
                required
              />
              <v-alert v-if="errorMessage" class="mb-3" role="alert" type="error" variant="tonal">
                {{ errorMessage }}
              </v-alert>
              <v-btn block color="primary" :loading="submitting" type="submit">
                {{ $t('security.completeTransition') }}
              </v-btn>
            </v-form>

            <v-form
              v-else-if="posture?.authState === 'mfa_pending'"
              aria-labelledby="mfa-challenge-heading"
              @submit.prevent="submitMFAChallenge"
            >
              <h2 id="mfa-challenge-heading" class="text-h6 mb-3">
                {{ recoveryMode ? $t('security.recoveryChallenge') : $t('security.mfaChallenge') }}
              </h2>
              <v-text-field
                v-model="challengeCode"
                :autocomplete="recoveryMode ? 'off' : 'one-time-code'"
                :label="recoveryMode ? $t('security.recoveryCode') : $t('security.authenticatorCode')"
                autocapitalize="characters"
                inputmode="numeric"
                required
              />
              <v-alert v-if="errorMessage" class="mb-3" role="alert" type="error" variant="tonal">
                {{ errorMessage }}
              </v-alert>
              <v-btn block color="primary" :loading="submitting" type="submit">
                {{ $t('security.verify') }}
              </v-btn>
              <v-btn block class="mt-2" variant="text" @click="recoveryMode = !recoveryMode">
                {{ recoveryMode ? $t('security.useAuthenticator') : $t('security.useRecoveryCode') }}
              </v-btn>
            </v-form>

            <v-alert v-else-if="!loading" type="info" variant="tonal">
              {{ $t('security.noTransition') }}
            </v-alert>
          </v-card-text>
          <v-card-actions>
            <v-btn variant="text" @click="logout">{{ $t('menu.logout') }}</v-btn>
          </v-card-actions>
        </v-card>
      </v-col>
    </v-row>
  </v-container>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { logout } from '@/shared/composables/useAuthOperations'
import {
  completeMFAChallenge,
  completeMFARecovery,
  completeRecoveryChallenge,
  getSecurityPosture,
  messageObject,
  transitionPassword,
  type SecurityPosture,
} from '@/shared/composables/useSecurityOperations'
import { useI18n } from 'vue-i18n'

const router = useRouter()
const { t } = useI18n()
const posture = ref<SecurityPosture | null>(null)
const loading = ref(true)
const submitting = ref(false)
const errorMessage = ref('')
const currentPassword = ref('')
const newUsername = ref('')
const newPassword = ref('')
const confirmPassword = ref('')
const challengeCode = ref('')
const recoveryMode = ref(false)

const transitionDescription = computed(() => {
  if (posture.value?.authState === 'password_reset') return t('security.passwordTransitionDescription')
  if (posture.value?.authState === 'mfa_pending') return t('security.mfaChallengeDescription')
  if (posture.value?.authState === 'mfa_recovery') return t('security.recoveryTransitionDescription')
  return t('security.loading')
})

onMounted(async () => {
  const response = await getSecurityPosture()
  posture.value = messageObject<SecurityPosture>(response)
  newUsername.value = posture.value?.username ?? ''
  loading.value = false
  if (posture.value?.authState === 'authenticated') {
    await router.replace('/')
  }
})

const clearSecrets = () => {
  currentPassword.value = ''
  newPassword.value = ''
  confirmPassword.value = ''
  challengeCode.value = ''
}

const submitPasswordTransition = async () => {
  errorMessage.value = ''
  if (newPassword.value !== confirmPassword.value) {
    errorMessage.value = t('security.passwordMismatch')
    return
  }
  submitting.value = true
  try {
    const response = posture.value?.authState === 'mfa_recovery'
      ? await completeMFARecovery({
          newUsername: newUsername.value,
          newPassword: newPassword.value,
        })
      : await transitionPassword({
          currentPassword: currentPassword.value,
          newUsername: newUsername.value,
          newPassword: newPassword.value,
        })
    if (!response.success) {
      errorMessage.value = response.msg
      return
    }
    clearSecrets()
    await router.replace('/')
  } finally {
    submitting.value = false
  }
}

const submitMFAChallenge = async () => {
  errorMessage.value = ''
  submitting.value = true
  try {
    const response = recoveryMode.value
      ? await completeRecoveryChallenge(challengeCode.value)
      : await completeMFAChallenge(challengeCode.value)
    challengeCode.value = ''
    if (!response.success) {
      errorMessage.value = response.msg
      return
    }
    const result = messageObject<{ state: SecurityPosture['authState'] }>(response)
    if (result?.state === 'mfa_recovery' && posture.value) {
      posture.value.authState = 'mfa_recovery'
      recoveryMode.value = false
      return
    }
    await router.replace('/')
  } finally {
    submitting.value = false
  }
}
</script>

<style scoped>
.security-transition {
  min-height: 100vh;
}
</style>
