import { createRouter, createWebHistory } from 'vue-router';
import OverviewPage from '@/pages/OverviewPage.vue';
import DailyPage from '@/pages/DailyPage.vue';
import RealtimePage from '@/pages/RealtimePage.vue';
import LogsPage from '@/pages/LogsPage.vue';
import SetupPage from '@/pages/SetupPage.vue';

const router = createRouter({
  history: createWebHistory(),
  routes: [
    {
      path: '/',
      name: 'overview',
      component: OverviewPage,
      meta: {
        sidebarLabelKey: 'app.sidebar.recentActive',
        sidebarHintKey: 'app.sidebar.recentActiveHint',
        mainClass: '',
      },
    },
    {
      path: '/daily',
      name: 'daily',
      component: DailyPage,
      meta: {
        sidebarLabelKey: 'app.menu.daily',
        sidebarHintKey: 'app.sidebar.dailyHint',
        mainClass: 'daily-page',
      },
    },
    {
      path: '/realtime',
      name: 'realtime',
      component: RealtimePage,
      meta: {
        sidebarLabelKey: 'app.menu.realtime',
        sidebarHintKey: 'app.sidebar.realtimeHint',
        mainClass: 'realtime-page',
      },
    },
    {
      path: '/logs',
      name: 'logs',
      component: LogsPage,
      meta: {
        sidebarLabelKey: 'app.menu.logs',
        sidebarHintKey: 'app.sidebar.logsHint',
        mainClass: 'logs-page',
      },
    },
    {
      path: '/settings',
      name: 'settings',
      component: SetupPage,
      props: { mode: 'manage' },
      meta: {
        sidebarLabelKey: 'app.menu.setup',
        sidebarHintKey: 'app.sidebar.setupHint',
        mainClass: 'setup-route',
      },
    },
  ],
  scrollBehavior() {
    return { top: 0 };
  },
});

export default router;
