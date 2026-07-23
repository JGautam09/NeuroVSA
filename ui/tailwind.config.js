/** @type {import('tailwindcss').Config} */
export default {
  content: [
    "./index.html",
    "./src/**/*.{js,ts,jsx,tsx}",
  ],
  theme: {
    extend: {
      colors: {
        vsaGreen: "#00ff66",
        vsaDarkGreen: "#003311",
        vsaBg: "#050B07",
        vsaPanel: "#0D1B12",
        vsaBorder: "#1A3A26"
      },
      fontFamily: {
        mono: ['Fira Code', 'JetBrains Mono', 'Courier New', 'monospace'],
      }
    },
  },
  plugins: [],
}
