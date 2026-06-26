export default {
  "paidSub": {
    "experimental": "آزمایشی",
    "secretboxWarning": "برای محیط عملیاتی، متغیر SUI_SECRETBOX_KEY را تنظیم کنید تا توکن‌های پرداخت با کلیدی خارج از پایگاه داده رمزگذاری شوند.",
    "tabs": {
      "bindings": "اتصال‌ها", "autoreg": "ثبت‌نام خودکار", "tariffs": "تعرفه‌ها",
      "payments": "پرداخت‌ها", "messages": "پیام‌ها", "orders": "سفارش‌ها", "bot": "ربات"
    },
    "active": "فعال",
    "disabled": "غیرفعال",
    "cols": {
      "client": "کاربر", "clientId": "شناسه کاربر", "description": "توضیحات",
      "telegramId": "شناسه تلگرام", "expiry": "انقضا", "status": "وضعیت",
      "name": "نام", "price": "قیمت", "stars": "استارز", "addDays": "+روز",
      "addTraffic": "+ترافیک", "enabled": "فعال", "id": "شناسه",
      "clientName": "نام کاربر", "provider": "ارائه‌دهنده", "amount": "مبلغ", "created": "ایجادشده"
    },
    "bindings": {
      "hint": "یک کاربر پنل را به شناسه تلگرام متصل کنید. سپس کاربر می‌تواند پیوند، کد QR، آمار و پرداخت را در ربات دریافت کند.",
      "add": "افزودن اتصال",
      "empty": "کاربری یافت نشد. در صفحه کاربران یک کاربر بسازید (یا ثبت‌نام خودکار را فعال کنید) و سپس آن را به شناسه تلگرام متصل کنید.",
      "none": "اتصالی وجود ندارد"
    },
    "autoreg": {
      "enable": "ثبت خودکار کاربران ناشناس", "inbounds": "ورودی‌ها برای کاربران جدید",
      "trialDays": "روزهای آزمایشی", "trialVolume": "ترافیک آزمایشی (گیگابایت، ۰ = نامحدود)",
      "maxClients": "حداکثر کاربران ثبت‌شده خودکار", "rateLimit": "محدودیت /start (در دقیقه)"
    },
    "tariffs": {
      "add": "افزودن تعرفه", "none": "تعرفه‌ای وجود ندارد", "new": "تعرفه جدید", "edit": "ویرایش تعرفه",
      "priceMajor": "قیمت (واحد اصلی)", "currency": "واحد پول", "starsAmount": "تعداد استارز (XTR)",
      "addDays": "+روز", "addTrafficGB": "+ترافیک (گیگابایت، ۰ = نامحدود)", "sort": "ترتیب", "enabledField": "فعال"
    },
    "payments": {
      "currency": "واحد پول پیش‌فرض (مانند RUB یا USD)", "orderTtl": "زمان اعتبار سفارش در انتظار (دقیقه)",
      "stars": "Telegram Stars (XTR)", "yookassa": "YooKassa", "yookassaToken": "provider_token یوکاسا (BotFather)",
      "stripe": "Stripe", "stripeToken": "provider_token استرایپ (BotFather)",
      "paymaster": "PayMaster", "paymasterToken": "provider_token پی‌مستر (BotFather)",
      "crypto": "CryptoBot", "cryptoToken": "توکن API کریپتوبات", "external": "پیوند پرداخت خارجی",
      "externalTemplate": "الگوی نشانی خارجی (https://… با {'{'}orderId{'}'} {'{'}amount{'}'} {'{'}currency{'}'} {'{'}clientId{'}'})"
    },
    "messages": {
      "greetingTitle": "خوشامدگویی در /start", "greetingHint": "هنگام بازکردن ربات به کاربر متصل نمایش داده می‌شود. برای متن پیش‌فرض خالی بگذارید.",
      "greetingLabel": "خوشامدگویی سفارشی", "broadcastTitle": "ارسال همگانی به کاربران",
      "broadcastHint": "یک اعلان را به همه کاربران تلگرام متصل ارسال می‌کند ({count} گیرنده).",
      "broadcastLabel": "متن اعلان", "sendAll": "ارسال به همه", "result": "ارسال‌شده: {sent} · ناموفق: {failed}",
      "confirmTitle": "اعلان ارسال شود؟", "confirmText": "این پیام برای همه {count} کاربر متصل ارسال می‌شود.", "send": "ارسال"
    },
    "orders": { "refund": "بازپرداخت" },
    "refund": {
      "title": "سفارش #{id} بازپرداخت شود؟", "starsNote": "استارز تلگرام به‌صورت خودکار بازپرداخت می‌شود.",
      "manualNote": "فقط سفارش را بازپرداخت‌شده علامت می‌زند؛ وجه را در پنل ارائه‌دهنده بازگردانید.",
      "revoke": "لغو روزها/ترافیک اعطاشده این سفارش", "done": "بازپرداخت انجام شد"
    },
    "bot": {
      "enable": "فعال‌کردن ربات کاربران", "pollTimeout": "مهلت long-poll (ثانیه)",
      "token": "توکن ربات (جدا از ربات مدیر)", "transportTitle": "اتصال / انتقال",
      "transportHint": "روش دسترسی این ربات به تلگرام؛ مستقل از ماژول تلگرام مدیر است.",
      "transport": "انتقال", "outbound": "خروجی (sing-box) — نیازمند اجرای هسته",
      "noOutbounds": "خروجی‌ای پیکربندی نشده", "proxyUrl": "نشانی پروکسی (http/https/socks5، خالی = مستقیم)",
      "proxyUser": "نام کاربری پروکسی (اختیاری)", "proxyPass": "گذرواژه پروکسی (اختیاری)"
    },
    "bindingDialog": {
      "addTitle": "افزودن اتصال", "editTitle": "اتصال تلگرام به {name}",
      "client": "کاربر", "tgId": "شناسه کاربر تلگرام (۰ = قطع اتصال)"
    },
    "unbind": {
      "title": "اتصال تلگرام از {name} قطع شود؟",
      "text": "این کار اتصال تلگرام کاربر #{clientId} (شناسه تلگرام {tgUserId}) را حذف می‌کند. کاربر در پنل باقی می‌ماند و فقط اتصال پاک می‌شود.",
      "confirm": "قطع اتصال"
    },
    "transportModes": { "proxy": "پروکسی", "outbound": "خروجی (sing-box)" }
  }
}
