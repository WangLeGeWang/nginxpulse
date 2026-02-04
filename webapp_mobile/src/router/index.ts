import { createRouter, createWebHistory } from 'vue-router';
import { getMobileBasePathWithSlash } from '@/utils';
import OverviewPage from '@mobile/pages/OverviewPage.vue';
import DailyPage from '@mobile/pages/DailyPage.vue';
import RealtimePage from '@mobile/pages/RealtimePage.vue';
import LogsPage from '@mobile/pages/LogsPage.vue';

const router = createRouter({
  history: createWebHistory(getMobileBasePathWithSlash()),
  routes: [
    {
      path: '/',
      name: 'overview',
      component: OverviewPage,
      meta: {
        mainClass: '',
      },
    },
    {
      path: '/daily',
      name: 'daily',
      component: DailyPage,
      meta: {
        mainClass: 'daily-page',
      },
    },
    {
      path: '/realtime',
      name: 'realtime',
      component: RealtimePage,
      meta: {
        mainClass: 'realtime-page',
      },
    },
    {
      path: '/logs',
      name: 'logs',
      component: LogsPage,
      meta: {
        mainClass: 'logs-page',
      },
    },
    {
      path: '/:pathMatch(.*)*',
      redirect: '/',
    },
  ],
  scrollBehavior() {
    return { top: 0 };
  },
});

export default router;
