# Telegram settings actions

Test saves a changed settings draft through the existing validated settings endpoint before sending the test message. A failed Save ends the action. Editing and Save/Test/Detect require a confirmed initial settings snapshot; failed loading leaves them disabled and offers an explicit reload. Controls are disabled while Save, Test or chat discovery is pending; a programmatic draft change or page teardown invalidates old results.

Detect chat performs one administrator-scoped POST to api/telegram/detect-chat (or apiv2/telegram/detect-chat for an API token). The optional JSON token is used only for that request. An omitted, blank or stored-secret placeholder token uses the encrypted saved bot token. Discovery uses the saved proxy/outbound transport and does not require an enabled notification channel. It fills the chat draft; Save persists that draft.

The service sends one Telegram getUpdates request with limit 100 and timeout 0, without offset, acknowledgement or a polling worker. Its context is at most ten seconds and the existing Bot API transport limits the response to 1 MiB. Redirects are rejected. Only successful HTTP and explicit Telegram ok=true with a JSON array of at most 100 updates can succeed.

Supported chat-bearing structures: message, edited_message, channel_post, edited_channel_post and my_chat_member. A usable chat has a nonzero integer ID and private, group, supergroup or channel type. Greatest nonnegative update_id wins; equal IDs use the last occurrence in the batch. This reports the last usable chat in the returned batch, not complete Telegram history. Chat IDs are decimal strings to preserve integer precision.

Responses contain success/chatId or a bounded errorClass. Audit events contain success/errorClass only. Supplied tokens, provider descriptions, update text and chat titles are never persisted or returned. Existing component installation/enabled-state, administrator authorization and browser CSRF owners secure both API variants. No real provider credentials are used by the tests.
