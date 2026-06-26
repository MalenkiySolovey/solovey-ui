<template>
    <v-container class="fill-height" style="margin-top: 100px;">
      <v-row justify="center" align="center">
        <v-col cols="12" sm="8" md="4">
          <v-card>
            <v-card-title class="headline" v-text="$t('login.title')"></v-card-title>
            <v-card-text>
              <v-form @submit.prevent="login" ref="form">
                <v-text-field v-model="username" :label="$t('login.username')" :rules="usernameRules" required></v-text-field>
                <v-text-field v-model="password" :label="$t('login.password')" :rules="passwordRules" type="password" required></v-text-field>
                <v-alert v-if="errorMessage" class="mt-1" density="compact" type="error" variant="tonal">
                  {{ errorMessage }}
                </v-alert>
                <v-btn :loading="loading" type="submit" color="primary" block class="mt-2" v-text="$t('actions.submit')"></v-btn>
              </v-form>
              <v-select
                density="compact"
                class="mt-2"
                hide-details
                variant="solo"
                :label="$t('menu.language')"
                :items="languages"
                v-model="$i18n.locale"
                @update:modelValue="changeLocale">
                <template v-slot:append>
                  <v-menu>
                    <template v-slot:activator="{ props }">
                      <v-btn icon :aria-label="$t('menu.theme')" :title="$t('menu.theme')" v-bind="props">
                        <v-icon>mdi-theme-light-dark</v-icon>
                      </v-btn>
                    </template>
                    <v-list>
                      <v-list-item
                        v-for="th in themes"
                        :key="th.value"
                        @click="changeTheme(th.value)"
                        :prepend-icon="th.icon"
                        :active="isActiveTheme(th.value)"
                      >
                        <v-list-item-title>{{ $t(`theme.${th.value}`) }}</v-list-item-title>
                      </v-list-item>
                    </v-list>
                  </v-menu>
                </template>
              </v-select>
            </v-card-text>
          </v-card>
        </v-col>
      </v-row>
    </v-container>
  </template>
  
<script lang="ts" setup>
import { useLoginPage } from '@/shared/composables/pages/useLoginPage'

const {
  changeLocale,
  changeTheme,
  errorMessage,
  isActiveTheme,
  languages,
  loading,
  login,
  password,
  passwordRules,
  themes,
  username,
  usernameRules,
} = useLoginPage()
</script>
