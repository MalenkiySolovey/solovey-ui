<template>
  <v-dialog transition="dialog-bottom-transition" width="420">
    <v-card class="rounded-lg">
      <v-card-title>
        {{ $t('admin.deleteAdmin') + " " + user.username }}
      </v-card-title>
      <v-divider></v-divider>
      <v-card-text>
        <v-alert
          v-if="error"
          type="error"
          density="compact"
          variant="tonal"
          class="mb-4"
        >
          {{ error }}
        </v-alert>
        <div class="mb-4">{{ $t('admin.deleteConfirm') }}</div>
        <v-text-field
          v-model="newData.currentPass"
          :label="$t('admin.oldPass')"
          type="password"
          autocomplete="current-password"
          required
        ></v-text-field>
        <v-text-field
          v-model="newData.stepUpCredential"
          :hint="$t('security.stepUpCredentialHint')"
          :label="$t('security.stepUpCredential')"
          persistent-hint
          type="password"
        ></v-text-field>
      </v-card-text>
      <v-card-actions>
        <v-spacer></v-spacer>
        <v-btn
          color="primary"
          variant="outlined"
          @click="closeModal"
        >
          {{ $t('actions.close') }}
        </v-btn>
        <v-btn
          color="error"
          variant="tonal"
          @click="deleteAdmin"
        >
          {{ $t('actions.del') }}
        </v-btn>
      </v-card-actions>
    </v-card>
  </v-dialog>
</template>

<script lang="ts">
import { i18n } from '@/locales'
import { isDeleteAdminFormValid } from '@/views/adminForms'

export default {
  props: ['visible', 'user'],
  data() {
    return {
      error: '',
      newData: {
        currentPass: '',
        stepUpCredential: '',
      },
    }
  },
  methods: {
    resetData() {
      this.error = ''
      this.newData.currentPass = ''
      this.newData.stepUpCredential = ''
    },
    closeModal() {
      this.resetData()
      this.$emit('close')
    },
    deleteAdmin() {
      this.error = ''
      if (!isDeleteAdminFormValid(this.newData)) {
        this.error = i18n.global.t('admin.deleteValidation') as string
        return
      }
      this.$emit('delete', {
        id: this.$props.user.id,
        currentPass: this.newData.currentPass,
        stepUpCredential: this.newData.stepUpCredential,
      })
    },
  },
  watch: {
    visible(newValue) {
      if (newValue) {
        this.resetData()
      }
    },
  },
}
</script>
