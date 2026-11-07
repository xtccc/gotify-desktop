import { Button } from "@astryxdesign/core/Button";
import { HStack } from "@astryxdesign/core/HStack";
import { Text } from "@astryxdesign/core/Text";
import { VStack } from "@astryxdesign/core/VStack";
import { MusicIcon } from "lucide-react";
import { useEffect, useState } from "react";
import { useT } from "./i18n";
import { Desktop } from "./mygo";
import { useAppState } from "./store";

/**
 * SoundPlayer is the page of the small window that shows the notification sound
 * while it plays, and stops it. Go shows and hides that window with the sound,
 * so this page only follows the state and counts the seconds left.
 */
export function SoundPlayer() {
  const state = useAppState();
  const t = useT();
  const sound = state?.sound ?? null;
  const [now, setNow] = useState(() => Date.now());

  // A notification sound can last a minute: the countdown follows it, and the
  // state itself, in case a change of it never reached this window.
  useEffect(() => {
    const id = window.setInterval(() => setNow(Date.now()), 500);
    return () => window.clearInterval(id);
  }, []);

  useEffect(() => {
    if (sound) return;
    const id = window.setTimeout(() => void Desktop.state(), 400);
    return () => window.clearTimeout(id);
  }, [sound]);

  const left = sound ? Math.max(0, Math.ceil((sound.startedMs + sound.durationMs - now) / 1000)) : 0;

  return (
    <div className="sound-player drag">
      <HStack gap={3} vAlign="center" className="sound-player-row">
        <MusicIcon size={18} className="sound-player-icon" />
        <VStack gap={0.5} className="grow">
          <Text weight="medium">{t.soundPlaying}</Text>
          <Text size="sm" color="secondary" className="sound-player-name">
            {sound ? sound.name : t.soundStopped}
          </Text>
        </VStack>
        <VStack gap={1} hAlign="end" className="sound-player-actions">
          {sound && sound.durationMs > 0 ? (
            <Text size="sm" color="secondary">
              {t.soundLeft(left)}
            </Text>
          ) : null}
          <Button
            label={t.soundStop}
            variant="secondary"
            size="sm"
            isDisabled={!sound}
            onClick={() => void Desktop.stopSound().catch((err) => console.error("stopping the sound:", err))}
          />
        </VStack>
      </HStack>
    </div>
  );
}
