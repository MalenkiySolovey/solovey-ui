export default {
  "paidSub": {
    "experimental": "thử nghiệm",
    "secretboxWarning": "Trong môi trường production, hãy đặt biến SUI_SECRETBOX_KEY để mã hóa token thanh toán bằng khóa nằm ngoài cơ sở dữ liệu.",
    "tabs": {
      "bindings": "Liên kết", "autoreg": "Đăng ký tự động", "tariffs": "Gói cước",
      "payments": "Thanh toán", "messages": "Tin nhắn", "orders": "Đơn hàng", "bot": "Bot"
    },
    "active": "đang hoạt động",
    "disabled": "đã tắt",
    "cols": {
      "client": "Máy khách", "clientId": "ID máy khách", "description": "Mô tả",
      "telegramId": "Telegram ID", "expiry": "Hết hạn", "status": "Trạng thái",
      "name": "Tên", "price": "Giá", "stars": "Stars", "addDays": "+Ngày",
      "addTraffic": "+Lưu lượng", "enabled": "Đã bật", "id": "ID",
      "clientName": "Tên máy khách", "provider": "Nhà cung cấp", "amount": "Số tiền", "created": "Đã tạo"
    },
    "bindings": {
      "hint": "Liên kết máy khách trong bảng điều khiển với Telegram ID. Sau đó máy khách có thể nhận liên kết, mã QR, thống kê và thanh toán trong bot.",
      "add": "Thêm liên kết",
      "empty": "Không tìm thấy máy khách. Hãy tạo máy khách trên trang Máy khách (hoặc bật Đăng ký tự động), rồi liên kết với Telegram ID tại đây.",
      "none": "Không có liên kết"
    },
    "autoreg": {
      "enable": "Tự động đăng ký người dùng chưa biết", "inbounds": "Inbound cho máy khách mới",
      "trialDays": "Số ngày dùng thử", "trialVolume": "Lưu lượng dùng thử (GB, 0 = không giới hạn)",
      "maxClients": "Số máy khách tự đăng ký tối đa", "rateLimit": "Giới hạn /start (mỗi phút)"
    },
    "tariffs": {
      "add": "Thêm gói cước", "none": "Không có gói cước", "new": "Gói cước mới", "edit": "Sửa gói cước",
      "priceMajor": "Giá (đơn vị chính)", "currency": "Tiền tệ", "starsAmount": "Số Stars (XTR)",
      "addDays": "+Ngày", "addTrafficGB": "+Lưu lượng (GB, 0 = không giới hạn)", "sort": "Thứ tự", "enabledField": "Đã bật"
    },
    "payments": {
      "currency": "Tiền tệ mặc định (ví dụ RUB, USD)", "orderTtl": "TTL đơn hàng đang chờ (phút)",
      "stars": "Telegram Stars (XTR)", "yookassa": "YooKassa", "yookassaToken": "provider_token YooKassa (BotFather)",
      "stripe": "Stripe", "stripeToken": "provider_token Stripe (BotFather)",
      "paymaster": "PayMaster", "paymasterToken": "provider_token PayMaster (BotFather)",
      "crypto": "CryptoBot", "cryptoToken": "Token API CryptoBot", "external": "Liên kết thanh toán bên ngoài",
      "externalTemplate": "Mẫu URL bên ngoài (https://… với {'{'}orderId{'}'} {'{'}amount{'}'} {'{'}currency{'}'} {'{'}clientId{'}'})"
    },
    "messages": {
      "greetingTitle": "Lời chào khi /start", "greetingHint": "Hiển thị cho máy khách đã liên kết khi mở bot. Để trống để dùng lời chào mặc định.",
      "greetingLabel": "Lời chào tùy chỉnh", "broadcastTitle": "Gửi tin cho tất cả máy khách",
      "broadcastHint": "Gửi thông báo một lần cho mọi người dùng Telegram đã liên kết ({count} người nhận).",
      "broadcastLabel": "Nội dung thông báo", "sendAll": "Gửi cho tất cả", "result": "Đã gửi: {sent} · lỗi: {failed}",
      "confirmTitle": "Gửi thông báo?", "confirmText": "Tin nhắn sẽ được gửi tới tất cả {count} máy khách đã liên kết.", "send": "Gửi"
    },
    "orders": { "refund": "Hoàn tiền" },
    "refund": {
      "title": "Hoàn tiền đơn hàng #{id}?", "starsNote": "Telegram Stars sẽ được hoàn tự động.",
      "manualNote": "Chỉ đánh dấu đơn hàng đã hoàn — hãy hoàn tiền trong bảng điều khiển của nhà cung cấp.",
      "revoke": "Thu hồi số ngày/lưu lượng đã cấp từ đơn hàng này", "done": "Đã xử lý hoàn tiền"
    },
    "bot": {
      "enable": "Bật bot cho máy khách", "pollTimeout": "Thời gian chờ long-poll (giây)",
      "token": "Token bot (tách biệt với bot quản trị)", "transportTitle": "Kết nối / truyền tải",
      "transportHint": "Cách bot này kết nối tới Telegram. Độc lập với mô-đun Telegram quản trị.",
      "transport": "Truyền tải", "outbound": "Outbound (sing-box) — yêu cầu core đang chạy",
      "noOutbounds": "Chưa cấu hình outbound", "proxyUrl": "URL proxy (http/https/socks5, để trống = trực tiếp)",
      "proxyUser": "Tên người dùng proxy (tùy chọn)", "proxyPass": "Mật khẩu proxy (tùy chọn)"
    },
    "bindingDialog": {
      "addTitle": "Thêm liên kết", "editTitle": "Liên kết Telegram với {name}",
      "client": "Máy khách", "tgId": "Telegram ID người dùng (0 = hủy liên kết)"
    },
    "unbind": {
      "title": "Hủy liên kết Telegram khỏi {name}?",
      "text": "Thao tác này xóa liên kết Telegram của máy khách #{clientId} (Telegram ID {tgUserId}). Máy khách vẫn còn trong bảng điều khiển; chỉ liên kết bị xóa.",
      "confirm": "Hủy liên kết"
    },
    "transportModes": { "proxy": "Proxy", "outbound": "Outbound (sing-box)" }
  }
}
