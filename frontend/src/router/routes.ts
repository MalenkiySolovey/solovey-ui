import type { RouteRecordRaw } from 'vue-router'
import { hostPath } from './hostContext'

export const routes: RouteRecordRaw[] = [
  {
    path: '/login',
    name: 'pages.login',
    component: () => import('@/views/Login.vue'),
  },
  {
    path: '/security-transition',
    name: 'pages.securityTransition',
    component: () => import('@/views/SecurityTransition.vue'),
  },
  {
    path: '/hosts/:hostId',
    name: 'main',
    component: () => import('@/layouts/AuthenticatedHost.vue'),
    meta: { requiresAuth: true },
    children: [
      { path: ':pathMatch(.*)*', name: 'pages.unavailable', component: () => import('@/views/HostFeatureUnavailable.vue') },
      {
        path: '',
        name: 'pages.home',
        component: () => import('@/views/Home.vue'),
      },
      {
        path: 'inbounds',
        name: 'pages.inbounds',
        component: () => import('@/views/Inbounds.vue'),
      },
      {
        path: 'clients',
        name: 'pages.clients',
        component: () => import('@/views/Clients.vue'),
      },
      {
        path: 'outbounds',
        name: 'pages.outbounds',
        component: () => import('@/views/Outbounds.vue'),
      },
      {
        path: 'services',
        name: 'pages.services',
        component: () => import('@/views/Services.vue'),
      },
      {
        path: 'endpoints',
        name: 'pages.endpoints',
        component: () => import('@/views/Endpoints.vue'),
      },
      {
        path: 'rules',
        name: 'pages.rules',
        component: () => import('@/views/Rules.vue'),
      },
      {
        path: 'tls',
        name: 'pages.tls',
        component: () => import('@/views/Tls.vue'),
      },
      {
        path: 'basics',
        redirect: to => ({ name: 'pages.settings', params: to.params, query: { ...to.query, tab: 'basics' }, hash: to.hash }),
      },
      {
        path: 'dns',
        name: 'pages.dns',
        component: () => import('@/views/Dns.vue'),
      },
      {
        path: 'sing-box-config',
        name: 'pages.singBoxConfig',
        component: () => import('@/views/SingBoxConfig.vue'),
      },
      {
        path: 'admins',
        name: 'pages.admins',
        component: () => import('@/views/Admins.vue'),
      },
      {
        path: 'security',
        name: 'pages.security',
        component: () => import('@/views/Security.vue'),
      },
      {
		path: 'ssh-management',
        name: 'pages.sshManagement',
        component: () => import('@/views/SSHManagement.vue'),
      },
	  {
		path: 'deployment',
		name: 'pages.deployment',
		component: () => import('@/views/Deployment.vue'),
	  },
      {
        path: 'operations',
        name: 'pages.operations',
        component: () => import('@/views/Operations.vue'),
      },
      {
        path: 'settings',
        name: 'pages.settings',
        component: () => import('@/views/Settings.vue'),
      },
      {
        path: 'support',
        name: 'pages.support',
        component: () => import('@/views/Donations.vue'),
      },
    ],
  },
  {
    path: '/:pathMatch(.*)*',
    redirect: to => ({ path: hostPath(to.path), query: to.query, hash: to.hash }),
  },
]
