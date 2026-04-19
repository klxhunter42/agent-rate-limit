type Lang = 'en' | 'th';

const translations: Record<Lang, Record<string, string>> = {
  en: {
    // Navigation
    'nav.overview': 'Overview',
    'nav.health': 'Health',
    'nav.model-limits': 'Model Limits',
    'nav.key-pool': 'Key Pool',
    'nav.analytics': 'Analytics',
    'nav.metrics': 'Metrics',
    'nav.controls': 'Controls',
    'nav.privacy': 'Privacy',
    'nav.providers': 'Providers',
    'nav.settings': 'Settings',

    // Overview
    'overview.status': 'Status',
    'overview.healthy': 'Healthy',
    'overview.unhealthy': 'Unhealthy',
    'overview.queue_depth': 'Queue Depth',
    'overview.total_requests': 'Total Requests',
    'overview.concurrency': 'Concurrency',
    'overview.quick_commands': 'Quick Commands',
    'overview.event_timeline': 'Event Timeline',

    // Health
    'health.title': 'System Health',
    'health.uptime': 'Uptime',
    'health.queue_depth': 'Queue Depth',

    // Analytics
    'analytics.title': 'Analytics',
    'analytics.total_tokens': 'Total Tokens',
    'analytics.total_cost': 'Total Cost',
    'analytics.input_cost': 'Input Cost',
    'analytics.output_cost': 'Output Cost',
    'analytics.avg_latency': 'Avg Latency',
    'analytics.usage_trend': 'Usage Trend',
    'analytics.cost_by_model': 'Cost by Model',
    'analytics.model_distribution': 'Model Distribution',
    'analytics.token_breakdown': 'Token Breakdown',

    // Key Pool
    'keypool.title': 'Key Pool',
    'keypool.total_keys': 'Total Keys',
    'keypool.active_keys': 'Active Keys',
    'keypool.cooldown_keys': 'In Cooldown',

    // Privacy
    'privacy.title': 'Privacy',
    'privacy.mode': 'Privacy Mode',
    'privacy.masked_requests': 'Masked Requests',
    'privacy.secrets_detected': 'Secrets Detected',
    'privacy.pii_detected': 'PII Detected',

    // Providers
    'providers.title': 'Providers',
    'providers.connect': 'Connect',
    'providers.disconnect': 'Disconnect',
    'providers.accounts': 'Accounts',
    'providers.default': 'Default',
    'providers.paused': 'Paused',
    'providers.active': 'Active',
    'providers.authenticating': 'Authenticating...',

    // Settings
    'settings.title': 'Settings',
    'settings.general': 'General',
    'settings.polling_interval': 'Polling Interval',
    'settings.theme': 'Theme',
    'settings.notifications': 'Notifications',
    'settings.language': 'Language',
    'settings.about': 'About',
    'settings.reset': 'Reset All Settings',

    // Common
    'common.loading': 'Loading...',
    'common.error': 'Error',
    'common.save': 'Save',
    'common.cancel': 'Cancel',
    'common.close': 'Close',
    'common.copy': 'Copy',
    'common.copied': 'Copied!',
    'common.refresh': 'Refresh',
    'common.enabled': 'Enabled',
    'common.disabled': 'Disabled',
    'common.dark': 'Dark',
    'common.light': 'Light',
    'common.connected': 'Connected',
    'common.disconnected': 'Disconnected',
  },
  th: {
    // Navigation
    'nav.overview': 'ภาพรวม',
    'nav.health': 'สถานะระบบ',
    'nav.model-limits': 'ขีดจำกัดโมเดล',
    'nav.key-pool': 'รวมคีย์',
    'nav.analytics': 'วิเคราะห์',
    'nav.metrics': 'เมตริกซ์',
    'nav.controls': 'ควบคุม',
    'nav.privacy': 'ความเป็นส่วนตัว',
    'nav.providers': 'ผู้ให้บริการ',
    'nav.settings': 'ตั้งค่า',

    // Overview
    'overview.status': 'สถานะ',
    'overview.healthy': 'ปกติ',
    'overview.unhealthy': 'มีปัญหา',
    'overview.queue_depth': 'คิวรอดำเนินการ',
    'overview.total_requests': 'คำขอทั้งหมด',
    'overview.concurrency': 'การทำงานพร้อมกัน',
    'overview.quick_commands': 'คำสั่งด่วน',
    'overview.event_timeline': 'ไทม์ไลน์เหตุการณ์',

    // Health
    'health.title': 'สถานะระบบ',
    'health.uptime': 'เวลาทำงาน',
    'health.queue_depth': 'คิวรอดำเนินการ',

    // Analytics
    'analytics.title': 'วิเคราะห์ข้อมูล',
    'analytics.total_tokens': 'โทเคนทั้งหมด',
    'analytics.total_cost': 'ค่าใช้จ่ายทั้งหมด',
    'analytics.input_cost': 'ค่าอินพุต',
    'analytics.output_cost': 'ค่าเอาท์พุต',
    'analytics.avg_latency': 'เวลาตอบสนองเฉลี่ย',
    'analytics.usage_trend': 'แนวโน้มการใช้งาน',
    'analytics.cost_by_model': 'ค่าใช้จ่ายตามโมเดล',
    'analytics.model_distribution': 'การกระจายโมเดล',
    'analytics.token_breakdown': 'สัดส่วนโทเคน',

    // Key Pool
    'keypool.title': 'รวมคีย์',
    'keypool.total_keys': 'คีย์ทั้งหมด',
    'keypool.active_keys': 'คีย์ที่ใช้งาน',
    'keypool.cooldown_keys': 'คีย์ที่พัก',

    // Privacy
    'privacy.title': 'ความเป็นส่วนตัว',
    'privacy.mode': 'โหมดความเป็นส่วนตัว',
    'privacy.masked_requests': 'คำขอที่ปิดบัง',
    'privacy.secrets_detected': 'ความลับที่ตรวจพบ',
    'privacy.pii_detected': 'ข้อมูลส่วนบุคคลที่ตรวจพบ',

    // Providers
    'providers.title': 'ผู้ให้บริการ',
    'providers.connect': 'เชื่อมต่อ',
    'providers.disconnect': 'ตัดการเชื่อมต่อ',
    'providers.accounts': 'บัญชี',
    'providers.default': 'ค่าเริ่มต้น',
    'providers.paused': 'หยุดชั่วคราว',
    'providers.active': 'ใช้งานอยู่',
    'providers.authenticating': 'กำลังยืนยันตัวตน...',

    // Settings
    'settings.title': 'ตั้งค่า',
    'settings.general': 'ทั่วไป',
    'settings.polling_interval': 'ระยะเวลารีเฟรช',
    'settings.theme': 'ธีม',
    'settings.notifications': 'การแจ้งเตือน',
    'settings.language': 'ภาษา',
    'settings.about': 'เกี่ยวกับ',
    'settings.reset': 'รีเซ็ตการตั้งค่าทั้งหมด',

    // Common
    'common.loading': 'กำลังโหลด...',
    'common.error': 'ข้อผิดพลาด',
    'common.save': 'บันทึก',
    'common.cancel': 'ยกเลิก',
    'common.close': 'ปิด',
    'common.copy': 'คัดลอก',
    'common.copied': 'คัดลอกแล้ว!',
    'common.refresh': 'รีเฟรช',
    'common.enabled': 'เปิดใช้งาน',
    'common.disabled': 'ปิดใช้งาน',
    'common.dark': 'มืด',
    'common.light': 'สว่าง',
    'common.connected': 'เชื่อมต่อแล้ว',
    'common.disconnected': 'ไม่ได้เชื่อมต่อ',
  },
};

export type { Lang };

export function t(key: string, lang?: Lang): string {
  const l = lang ?? getCurrentLang();
  return translations[l]?.[key] ?? translations.en[key] ?? key;
}

export function getCurrentLang(): Lang {
  return (localStorage.getItem('arl-lang') as Lang) || 'en';
}

export function setLang(lang: Lang): void {
  localStorage.setItem('arl-lang', lang);
}

export function useTranslation() {
  const [lang, setLangState] = React.useState<Lang>(getCurrentLang);

  React.useEffect(() => {
    const handler = () => setLangState(getCurrentLang());
    window.addEventListener('arl:lang-changed', handler);
    window.addEventListener('storage', handler);
    return () => {
      window.removeEventListener('arl:lang-changed', handler);
      window.removeEventListener('storage', handler);
    };
  }, []);

  const changeLang = (l: Lang) => {
    setLang(l);
    setLangState(l);
    window.dispatchEvent(new CustomEvent('arl:lang-changed', { detail: l }));
  };

  return { t: (key: string) => t(key, lang), lang, setLang: changeLang };
}

import * as React from 'react';
