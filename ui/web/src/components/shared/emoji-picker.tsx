import { useState, useLayoutEffect, useRef, useCallback } from "react";
import { Bot, Pencil } from "lucide-react";
import { Input } from "@/components/ui/input";
import { Popover, PopoverTrigger, PopoverContent } from "@/components/ui/popover";

const EMOJI_CATEGORIES = [
  {
    label: "Smileys",
    emojis: ["😀", "😃", "😄", "😁", "😆", "😅", "🤣", "😂", "🙂", "😊", "😇", "🥰", "😍", "🤩", "😘", "😋", "😛", "😜", "🤪", "😝", "🤑", "🤗", "🤭", "🤫", "🤔", "😐", "😑", "😏", "😒", "🙄", "😬", "😌", "😔", "😪", "😴", "😷", "🤒", "🤕", "😵", "🤯", "🤠", "🥳", "😎", "🤓", "🧐", "😕", "😟", "😮", "😲", "😳", "🥺", "😦", "😨", "😰", "😥", "😢", "😭", "😱", "😖", "😣", "😞", "😓", "😩", "😫", "😤", "😡", "🤬", "😈", "👿", "💀", "💩", "🤡", "👻", "👽", "👾", "🤖"],
  },
  {
    label: "Animals",
    emojis: ["😺", "😸", "😹", "😻", "🙀", "😿", "😾", "🐶", "🐱", "🐭", "🐹", "🐰", "🦊", "🐻", "🐼", "🐨", "🐯", "🦁", "🐮", "🐷", "🐸", "🐵", "🙈", "🙉", "🙊", "🐔", "🐧", "🐦", "🦆", "🦅", "🦉", "🐺", "🐗", "🐴", "🦄", "🐝", "🐛", "🦋", "🐌", "🐞", "🐢", "🐍", "🦎", "🦖", "🦕", "🐙", "🦑", "🐬", "🐳", "🐋", "🦈", "🐊", "🐆", "🦓", "🦍", "🐘", "🦛", "🦏", "🐫", "🦒", "🐃", "🐂", "🐄", "🐎", "🐑", "🐐", "🦌", "🐕", "🐩", "🐈", "🐓", "🦃", "🦚", "🦜", "🦢", "🕊️", "🐇", "🦝", "🦔", "🐿️"],
  },
  {
    label: "Objects",
    emojis: ["🎭", "🎨", "🎬", "🎤", "🎧", "🎵", "🎹", "🥁", "🎸", "🎲", "🎯", "🎳", "🎮", "🧩", "🚀", "✈️", "🚁", "🚂", "🚗", "🚌", "🏎️", "🚓", "🚑", "🚒", "🚚", "🛵", "🏍️", "🚲", "⚡", "💥", "🔥", "🌟", "💫", "✨", "⭐", "🌈", "☀️", "🌙", "⚙️", "🔧", "🔨", "💡", "🔑", "🏆", "🏅", "🥇", "🎪", "🎡", "🎢", "🎠", "🎈", "🎉", "🎊", "🎁", "🎀", "👑", "💎", "🛡️", "⚔️", "🏹", "🔮"],
  },
  {
    label: "Symbols",
    emojis: ["❤️", "🧡", "💛", "💚", "💙", "💜", "🖤", "🤍", "💔", "❣️", "💕", "💞", "💓", "💗", "💖", "💘", "💝", "💟", "☮️", "✝️", "☪️", "🕉️", "☸️", "✡️", "🔯", "🕎", "☯️", "☦️", "🛐", "⛎", "♈", "♉", "♊", "♋", "♌", "♍", "♎", "♏", "♐", "♑", "♒", "♓", "🆔", "⚛️", "🉑", "☢️", "☣️", "📴", "📳", "🈶", "🈚", "🈸", "🈺", "🈷️", "✴️", "🆚", "💮", "🉐", "㊙️", "㊗️", "🈴", "🈵", "🈹", "🈲", "🅰️", "🅱️", "🆎", "🆑", "🅾️", "🆘", "⛔", "📛", "🚫", "💯", "💢", "♨️", "🚷", "🚯", "🚳", "🔞", "📵", "🚭", "❗", "❕", "❓", "❔", "‼️", "⁉️", "🔅", "🔆", "〽️", "⚠️", "🚸", "🔱", "⚜️", "🔰", "♻️", "✅", "🈯", "💹", "❇️", "✳️", "❎", "🌐", "💠", "Ⓜ️", "🌀", "💤", "🏧", "🚾", "♿", "🅿️", "🈳", "🈂️", "🛂", "🛃", "🛄", "🛅", "🚹", "🚺", "🚼", "🚻", "🚮", "🎦", "📶", "🈁", "🔣", "ℹ️", "🔤", "🔡", "🔠", "🆖", "🆗", "🆙", "🆒", "🆕", "🆓", "0️⃣", "1️⃣", "2️⃣", "3️⃣", "4️⃣", "5️⃣", "6️⃣", "7️⃣", "8️⃣", "9️⃣", "🔟", "🔢", "#️⃣", "*️⃣", "⏏️", "▶️", "⏸️", "⏯️", "⏹️", "⏺️", "⏭️", "⏮️", "⏩", "⏪", "🔀", "🔁", "🔂", "◀️", "🔼", "🔽"],
  },
];

/** Flatten all emojis into a single searchable set */
const ALL_EMOJIS = EMOJI_CATEGORIES.flatMap((c) => c.emojis);

/** Extract the first emoji grapheme cluster from a string, or return empty. */
function extractSingleEmoji(str: string): string {
  const match = str.match(/\p{Emoji_Presentation}(\u200D\p{Emoji_Presentation})*/u)
    ?? str.match(/\p{Extended_Pictographic}(\uFE0F?\u200D\p{Extended_Pictographic})*/u);
  return match?.[0] ?? "";
}

interface EmojiPickerProps {
  value: string;
  onChange: (emoji: string) => void;
  size?: "sm" | "md" | "lg";
  className?: string;
}

export function EmojiPicker({ value, onChange, size = "lg", className }: EmojiPickerProps) {
  const [open, setOpen] = useState(false);
  const [search, setSearch] = useState("");
  const inputRef = useRef<HTMLInputElement>(null);

  const handleSelect = useCallback((emoji: string) => {
    onChange(emoji);
    setOpen(false);
    setSearch("");
  }, [onChange]);

  const handleManualInput = useCallback((e: React.ChangeEvent<HTMLInputElement>) => {
    const emoji = extractSingleEmoji(e.target.value);
    if (emoji) {
      onChange(emoji);
      setOpen(false);
      setSearch("");
    } else {
      setSearch(e.target.value);
    }
  }, [onChange]);

  useLayoutEffect(() => {
    if (open) {
      // Focus search input after popover opens
      requestAnimationFrame(() => inputRef.current?.focus());
    }
  }, [open]);

  const filteredCategories = search
    ? [{ label: "Results", emojis: ALL_EMOJIS.filter((e) => e.includes(search)) }]
    : EMOJI_CATEGORIES;

  const sizeClasses = size === "sm"
    ? "h-8 w-8"
    : size === "md"
      ? "h-10 w-10"
      : "h-12 w-12";
  const emojiTextClasses = size === "sm"
    ? "text-base"
    : size === "md"
      ? "text-lg"
      : "text-2xl";
  const iconClasses = size === "sm"
    ? "h-4 w-4"
    : size === "md"
      ? "h-5 w-5"
      : "h-6 w-6";
  const pencilSize = size === "sm" ? "size-3" : "size-3.5";

  return (
    <Popover open={open} onOpenChange={(v) => { setOpen(v); if (!v) setSearch(""); }}>
      <PopoverTrigger asChild>
        <button
          type="button"
          className={`group relative flex ${sizeClasses} items-center justify-center rounded-xl bg-primary/10 text-primary hover:bg-primary/20 hover:ring-2 hover:ring-primary/30 transition-all ${className ?? ""}`}
        >
          {value ? (
            <span className={emojiTextClasses + " leading-none"}>{value}</span>
          ) : (
            <Bot className={iconClasses + " text-muted-foreground"} />
          )}
          <span className={`absolute -bottom-0.5 -right-0.5 flex ${pencilSize} items-center justify-center rounded-full bg-primary text-primary-foreground opacity-0 group-hover:opacity-100 transition-opacity`}>
            <Pencil className="size-2" />
          </span>
        </button>
      </PopoverTrigger>
      <PopoverContent
        align="start"
        sideOffset={8}
        className="w-72 p-2 max-h-80 overflow-hidden flex flex-col"
      >
        {/* Search / paste input */}
        <Input
          ref={inputRef}
          value={search}
          onChange={handleManualInput}
          placeholder="Search or paste emoji..."
          className="mb-2 h-8 text-sm"
        />
        {/* Emoji grid */}
        <div className="flex-1 overflow-y-auto overscroll-contain">
          {filteredCategories.map((cat) => (
            <div key={cat.label} className="mb-1.5">
              <div className="text-2xs text-muted-foreground px-1 mb-0.5 font-medium">{cat.label}</div>
              <div className="grid grid-cols-8 gap-0.5">
                {cat.emojis.map((emoji, i) => (
                  <button
                    key={`${emoji}-${i}`}
                    type="button"
                    className="flex h-8 w-8 items-center justify-center rounded-md text-lg hover:bg-accent transition-colors"
                    onClick={() => handleSelect(emoji)}
                  >
                    {emoji}
                  </button>
                ))}
              </div>
            </div>
          ))}
          {search && filteredCategories[0]?.emojis.length === 0 && (
            <div className="py-4 text-center text-xs text-muted-foreground">No matching emojis</div>
          )}
        </div>
      </PopoverContent>
    </Popover>
  );
}

export { extractSingleEmoji };
