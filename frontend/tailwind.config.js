/** @type {import('tailwindcss').Config} */
export default {
  content: ['./index.html', './src/**/*.{vue,js,ts,jsx,tsx}'],
  darkMode: 'class',
  theme: {
    extend: {
      colors: {
        primary: {
          50: '#eef6f1',
          100: '#dcebe1',
          200: '#bfd8c6',
          300: '#97bda6',
          400: '#6e9e83',
          500: '#4c7d64',
          600: '#3f6854',
          700: '#345446',
          800: '#2c4439',
          900: '#263730',
          950: '#141d1a'
        },
        accent: {
          50: '#fff5f1',
          100: '#ffe7de',
          200: '#ffd0bd',
          300: '#ffb194',
          400: '#f38a6e',
          500: '#e16d4f',
          600: '#c5563b',
          700: '#a24632',
          800: '#7b372a',
          900: '#5f2c23',
          950: '#341610'
        },
        dark: {
          50: '#f5f5f3',
          100: '#ebebe8',
          200: '#d7d6d1',
          300: '#b5b4ad',
          400: '#8b8a81',
          500: '#6b6a61',
          600: '#56554e',
          700: '#45443e',
          800: '#2f2f2b',
          900: '#21211e',
          950: '#171714'
        }
      },
      fontFamily: {
        sans: [
          '"Segoe UI Variable"',
          'Aptos',
          '"Segoe UI"',
          '"PingFang SC"',
          '"Microsoft YaHei"',
          'system-ui',
          'sans-serif'
        ],
        mono: ['ui-monospace', '"Cascadia Code"', '"SFMono-Regular"', 'Consolas', 'monospace']
      },
      boxShadow: {
        glass: '0 18px 40px rgba(23, 23, 20, 0.08)',
        'glass-sm': '0 10px 24px rgba(23, 23, 20, 0.06)',
        glow: '0 10px 30px rgba(63, 104, 84, 0.18)',
        'glow-lg': '0 18px 44px rgba(63, 104, 84, 0.22)',
        card: '0 12px 32px rgba(23, 23, 20, 0.06), 0 1px 2px rgba(23, 23, 20, 0.08)',
        'card-hover': '0 22px 48px rgba(23, 23, 20, 0.12), 0 4px 12px rgba(23, 23, 20, 0.08)',
        'inner-glow': 'inset 0 1px 0 rgba(255, 255, 255, 0.12)'
      },
      backgroundImage: {
        'gradient-radial': 'radial-gradient(var(--tw-gradient-stops))',
        'gradient-primary': 'linear-gradient(135deg, #4c7d64 0%, #345446 100%)',
        'gradient-accent': 'linear-gradient(135deg, #f38a6e 0%, #c5563b 100%)',
        'gradient-dark': 'linear-gradient(135deg, #2f2f2b 0%, #171714 100%)',
        'surface-grid':
          'linear-gradient(rgba(63, 104, 84, 0.06) 1px, transparent 1px), linear-gradient(90deg, rgba(63, 104, 84, 0.06) 1px, transparent 1px)',
        'panel-wash':
          'radial-gradient(at 0% 0%, rgba(76, 125, 100, 0.14) 0px, transparent 45%), radial-gradient(at 100% 100%, rgba(225, 109, 79, 0.12) 0px, transparent 40%)',
        'mesh-gradient':
          'radial-gradient(at 20% 10%, rgba(76, 125, 100, 0.12) 0px, transparent 45%), radial-gradient(at 80% 0%, rgba(225, 109, 79, 0.08) 0px, transparent 40%), linear-gradient(rgba(63, 104, 84, 0.04) 1px, transparent 1px), linear-gradient(90deg, rgba(63, 104, 84, 0.04) 1px, transparent 1px)'
      },
      animation: {
        'fade-in': 'fadeIn 0.3s ease-out',
        'slide-up': 'slideUp 0.3s ease-out',
        'slide-down': 'slideDown 0.3s ease-out',
        'slide-in-right': 'slideInRight 0.3s ease-out',
        'scale-in': 'scaleIn 0.2s ease-out',
        'pulse-slow': 'pulse 3s cubic-bezier(0.4, 0, 0.6, 1) infinite',
        shimmer: 'shimmer 2s linear infinite',
        glow: 'glow 2s ease-in-out infinite alternate'
      },
      keyframes: {
        fadeIn: {
          '0%': { opacity: '0' },
          '100%': { opacity: '1' }
        },
        slideUp: {
          '0%': { opacity: '0', transform: 'translateY(10px)' },
          '100%': { opacity: '1', transform: 'translateY(0)' }
        },
        slideDown: {
          '0%': { opacity: '0', transform: 'translateY(-10px)' },
          '100%': { opacity: '1', transform: 'translateY(0)' }
        },
        slideInRight: {
          '0%': { opacity: '0', transform: 'translateX(20px)' },
          '100%': { opacity: '1', transform: 'translateX(0)' }
        },
        scaleIn: {
          '0%': { opacity: '0', transform: 'scale(0.95)' },
          '100%': { opacity: '1', transform: 'scale(1)' }
        },
        shimmer: {
          '0%': { backgroundPosition: '-200% 0' },
          '100%': { backgroundPosition: '200% 0' }
        },
        glow: {
          '0%': { boxShadow: '0 10px 24px rgba(63, 104, 84, 0.16)' },
          '100%': { boxShadow: '0 16px 34px rgba(63, 104, 84, 0.24)' }
        }
      },
      backdropBlur: {
        xs: '2px'
      },
      borderRadius: {
        '4xl': '2rem'
      }
    }
  },
  plugins: []
}
