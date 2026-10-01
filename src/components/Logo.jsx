export default function Logo({ size = 34 }) {
  return (
    <svg className="logo-mark" width={size} height={size} viewBox="0 0 64 64" aria-hidden>
      <defs>
        <linearGradient id="lg" x1="8" y1="4" x2="58" y2="62" gradientUnits="userSpaceOnUse">
          <stop offset="0" stopColor="#6a5cff" />
          <stop offset="1" stopColor="#2f9bff" />
        </linearGradient>
        <linearGradient id="lh" x1="0" y1="0" x2="0" y2="1">
          <stop offset="0" stopColor="#fff" stopOpacity=".5" />
          <stop offset=".6" stopColor="#fff" stopOpacity="0" />
        </linearGradient>
      </defs>
      <rect width="64" height="64" rx="18" fill="url(#lg)" />
      <rect x="1" y="1" width="62" height="62" rx="17" fill="none" stroke="#fff" strokeOpacity=".5" strokeWidth="2" />
      <path d="M0 0H64V30C46 36 18 36 0 28Z" fill="url(#lh)" opacity=".55" />
      <path d="M12 51C14 32 30 50 34 33C36 24 42 26 46 20" fill="none" stroke="#fff" strokeOpacity=".8" strokeWidth="3.6" strokeLinecap="round" strokeDasharray="0.1 7.6" />
      <circle cx="12" cy="51" r="3.6" fill="#fff" fillOpacity=".7" />
      <path d="M47 9C47.8 16 50 18.2 57 19C50 19.8 47.8 22 47 29C46.2 22 44 19.8 37 19C44 18.2 46.2 16 47 9Z" fill="#fff" />
    </svg>
  )
}
