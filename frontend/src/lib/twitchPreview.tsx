import type { DiscordPreviewMessage } from '../types'

/**
 * A mock of how a Twitch chat announcement shows up in chat: the
 * "Announcement" header with Twitch's purple accent, then the channel
 * name and message. Twitch renders announcements as plain text, so
 * unlike DiscordChannelPreview there's no markup to interpret.
 */
export function TwitchChatPreview({
  messages,
  channel,
}: {
  messages: DiscordPreviewMessage[]
  channel: string
}) {
  return (
    <div className="twitch-preview" aria-label="Twitch chat preview">
      {messages.map((m) => (
        <div key={`${m.kind}-${m.label}`}>
          <div className="twitch-preview-label">{m.label}</div>
          <div className="twitch-announcement">
            <div className="twitch-announcement-header">📣 Announcement</div>
            <div className="twitch-announcement-body">
              <span className="twitch-username">{channel || 'yourchannel'}</span>
              <span>: {m.content}</span>
            </div>
          </div>
        </div>
      ))}
    </div>
  )
}
