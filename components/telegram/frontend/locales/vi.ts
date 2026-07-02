export default {
  "telegram": {
    "title": "Telegram",
    "enabled": "Bật",
    "botToken": "Mã thông báo bot",
    "chatId": "Mã chat",
    "proxyUrl": "URL proxy",
    "proxyUsername": "Tên đăng nhập proxy",
    "proxyPassword": "Mật khẩu proxy",
    "cpuThreshold": "Ngưỡng CPU",
    "notifyCpu": "Cảnh báo CPU",
    "report": "Báo cáo",
    "reportCron": "Cron báo cáo",
    "securityWarning": "Telegram bị tắt theo mặc định. URL proxy được xác thực phía máy chủ; mã thông báo và thông tin xác thực proxy được lưu dưới dạng trường bí mật.",
    "transport": "Phương thức kết nối",
    "transportProxy": "Proxy",
    "transportOutbound": "Outbound sing-box",
    "outboundLabel": "Outbound yêu cầu lõi đang chạy",
    "noOutbounds": "Chưa cấu hình outbound",
    "hint": {
      "chatId": "ID số của cuộc trò chuyện hoặc người dùng Telegram nhận cảnh báo. Có thể tìm qua @userinfobot.",
      "cpuThreshold": "Gửi cảnh báo khi CPU duy trì trên tỷ lệ này. Mặc định: 90. Phạm vi: 1-100.",
      "reportCron": "Lịch cron 5 trường cho báo cáo, ví dụ 0 9 * * *. Để trống để tắt.",
      "transport": "Cách bot kết nối Telegram: URL proxy hoặc outbound sing-box. Mặc định: Proxy.",
      "backupMaxSize": "Bỏ qua bản sao lưu nếu cơ sở dữ liệu mã hóa vượt kích thước này. Mặc định: 45 MB."
    },
    "backup": {
      "title": "Sao lưu cơ sở dữ liệu vào Telegram",
      "enabled": "Sao lưu Telegram",
      "passphrase": "Cụm mật khẩu sao lưu",
      "passphraseHint": "Cụm mật khẩu này mã hóa tất cả các bản sao lưu được gửi đến Telegram và các bản sao lưu thủ công tùy chọn từ Backup & Restore. Hãy ghi nhớ nó: nếu không có nó, tệp Telegram không thể khôi phục trên bảng điều khiển hoặc cục bộ. Khôi phục bảng điều khiển: tải tệp lên trong Backup & Restore và nhập cụm mật khẩu này. Giải mã cục bộ: dùng lệnh con s-ui decrypt-backup.",
      "passphraseMinLength": "Sử dụng ít nhất 12 ký tự.",
      "cron": "Cron sao lưu",
      "cronInvalid": "Sử dụng biểu thức cron 5 trường; bước phải tối thiểu 1 phút.",
      "schedule": {
        "title": "Tần suất sao lưu",
        "manual": "Chỉ thủ công",
        "every15m": "Mỗi 15 phút",
        "every30m": "Mỗi 30 phút",
        "hourly": "Mỗi giờ",
        "every6h": "Mỗi 6 giờ",
        "every12h": "Mỗi 12 giờ",
        "daily3": "Hàng ngày lúc 03:00",
        "custom": "Tùy chỉnh",
        "advanced": "Cron nâng cao",
        "customValue": "Mỗi",
        "customUnit": "Đơn vị",
        "advancedCron": "Cron nâng cao",
        "minutes": "phút",
        "hours": "giờ",
        "errors": {
          "customMinutesRange": "Sử dụng 1-59 phút.",
          "customHoursRange": "Sử dụng 1-23 giờ.",
          "advancedCronInvalid": "Sử dụng biểu thức cron 5 trường; bước phải tối thiểu 1 phút."
        }
      },
      "excludeTables": "Bảng loại trừ",
      "maxSize": "Kích thước tối đa",
      "sendNow": "Gửi ngay",
      "tables": {
        "stats": "Thống kê",
        "client_ips": "IP khách hàng",
        "audit_events": "Sự kiện kiểm toán",
        "changes": "Thay đổi"
      }
    }
  }
}
