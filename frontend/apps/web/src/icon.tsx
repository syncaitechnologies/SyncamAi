const paths: Record<string, string> = {
  grid: "M4 4h6v6H4zM14 4h6v6h-6zM4 14h6v6H4zM14 14h6v6h-6z",
  bell: "M18 8a6 6 0 0 0-12 0c0 7-3 7-3 9h18c0-2-3-2-3-9M10 21h4",
  camera:
    "M4 7h3l1.5-2h7L17 7h3v11H4zM12 15.5a3.5 3.5 0 1 0 0-7 3.5 3.5 0 0 0 0 7Z",
  chart: "M4 19V5M4 19h17M8 16v-4M12 16V8M16 16v-7M20 16v-3",
  file: "M6 3h9l4 4v14H6zM14 3v5h5M9 13h6M9 17h6",
  settings:
    "M12 15.5a3.5 3.5 0 1 0 0-7 3.5 3.5 0 0 0 0 7ZM19.4 15a1.7 1.7 0 0 0 .3 1.9l.1.1-1.7 1.7-.1-.1a1.7 1.7 0 0 0-1.9-.3 1.7 1.7 0 0 0-1 1.6v.1h-2.4v-.1a1.7 1.7 0 0 0-1-1.6 1.7 1.7 0 0 0-1.9.3l-.1.1L8 17l.1-.1a1.7 1.7 0 0 0 .3-1.9 1.7 1.7 0 0 0-1.6-1H6.7v-2.4h.1a1.7 1.7 0 0 0 1.6-1 1.7 1.7 0 0 0-.3-1.9L8 8.6l1.7-1.7.1.1a1.7 1.7 0 0 0 1.9.3 1.7 1.7 0 0 0 1-1.6v-.1h2.4v.1a1.7 1.7 0 0 0 1 1.6 1.7 1.7 0 0 0 1.9-.3l.1-.1 1.7 1.7-.1.1a1.7 1.7 0 0 0-.3 1.9 1.7 1.7 0 0 0 1.6 1h.1V14h-.1a1.7 1.7 0 0 0-1.6 1Z",
  search: "m20 20-4.5-4.5M11 18a7 7 0 1 1 0-14 7 7 0 0 1 0 14Z",
  chevron: "m9 18 6-6-6-6",
  arrow: "M5 12h14m-6-6 6 6-6 6",
  check: "m5 12 4 4L19 6",
  more: "M5 12h.01M12 12h.01M19 12h.01",
  shield: "M12 3 20 6v5c0 5-3.4 8.5-8 10-4.6-1.5-8-5-8-10V6z",
  close: "m6 6 12 12M18 6 6 18",
  clock: "M12 7v5l3 2M21 12a9 9 0 1 1-18 0 9 9 0 0 1 18 0Z",
  external: "M14 4h6v6M20 4l-9 9M18 13v6H5V6h6",
  trend: "M4 17l5-5 4 3 7-9M16 6h4v4",
  activity: "M4 13h3l2-6 4 12 3-8h4",
  users: "M16 20v-1.5a4.5 4.5 0 0 0-4.5-4.5h-3A4.5 4.5 0 0 0 4 18.5V20M10 10a3 3 0 1 0 0-6 3 3 0 0 0 0 6ZM17 11a3 3 0 0 0 0-6M19 20v-1.5a4.5 4.5 0 0 0-3-4.2",
};

export function Icon({ name, size = 18 }: { name: string; size?: number }) {
  return (
    <svg
      aria-hidden="true"
      className="icon"
      height={size}
      viewBox="0 0 24 24"
      width={size}
    >
      <path d={paths[name] ?? paths.more} />
    </svg>
  );
}
