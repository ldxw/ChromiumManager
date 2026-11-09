export const languages = [
  'ca-ES',
  'prs-AF',
  'ps-AF',
  'sq-AL',
  'hy-AM',
  'pt-PT',
  'en-US',
  'es-AR',
  'de-AT',
  'ar-AE',
  'sv-SE',
  'fi-FI',
  'az-Latn-AZ',
  'az-Cyrl-AZ',
  'bs-BA',
  'hr-BA',
  'sr-BA',
  'nl-NL',
  'bn-BD',
  'en-AU',
  'fr-FR',
  'bg-BG',
  'ar-BH',
  'quz-BO',
  'fr-BE',
  'pt-BR',
  'tn-ZA',
  'be-BY',
  'en-BZ',
  'fr-CA',
  'ms-BN',
  'arn-CL',
  'es-CO',
  'es-CR',
  'es-ES',
  'el-GR',
  'tr-TR',
  'cs-CZ',
  'de-DE',
  'ar-SA',
  'da-DK',
  'es-DO',
  'ar-DZ',
  'quz-EC',
  'et-EE',
  'ar-EG',
  'se-FI',
  'de-CH',
  'fo-FO',
  'ka-GE',
  'kl-GL',
  'qut-GT',
  'zh-HK',
  'es-HN',
  'hr-HR',
  'hu-HU',
  'he-IL',
  'ga-IE',
  'id-ID',
  'en-GB',
  'hi-IN',
  'is-IS',
  'fa-IR',
  'ar-IQ',
  'it-IT',
  'en-JM',
  'ar-JO',
  'ja-JP',
  'km-KH',
  'ar-KW',
  'de-LI',
  'si-LK',
  'ar-LB',
  'lt-LT',
  'de-LU',
  'lv-LV',
  'ar-LY',
  'ar-MA',
  'fr-MC',
  'ro-RO',
  'sr-Latn-ME',
  'mk-MK',
  'mt-MT',
  'dv-MV',
  'es-MX',
  'ms-MY',
  'ha-Latn-NG',
  'es-NI',
  'fy-NL',
  'se-NO',
  'ne-NP',
  'ar-OM',
  'es-PA',
  'quz-PE',
  'zh-MO',
  'fil-PH',
  'pl-PL',
  'ur-PK',
  'es-PR',
  'ar-QA',
  'sr-Latn-RS',
  'es-PY',
  'ru-RU',
  'se-SE',
  'en-SG',
  'sl-SI',
  'nn-no',
  'sk-SK',
  'wo-SN',
  'es-SV',
  'ar-SY',
  'th-TH',
  'ar-TN',
  'sw-KE',
  'uk-UA',
  'es-UY',
  'es-VE',
  'vi-VN',
  'ar-YE',
  'en-ZW',
  'nso-ZA',
  'zh-CN',
  'am-ET',
  'en-TT',
  'zh-TW',
  'en-NZ',
  'mi-NZ',
  'ko-KR',
  'lo-LA',
  'ky-KG',
  'kk-KZ',
  'tg-Cyrl-TJ',
  'tk-TM',
  'uz-Latn-UZ',
  'mn-Mong',
  'rw-RW'
]

export const timezones = Intl.supportedValuesOf('timeZone')

// 指纹默认值：新建配置、以及经 API / MCP 创建的配置都用这一组。
export const DEFAULT_PLATFORM = 'windows'
export const DEFAULT_BRAND = 'Chrome'
export const DEFAULT_HARDWARE_CONCURRENCY = '8'
export const DEFAULT_DEVICE_MEMORY = '8'
export const DEFAULT_SCREEN = '1920x1080'

// 操作系统 / 浏览器品牌的可选值。操作系统的 label 与 value 不同名。
export const platformOptions = [
  { label: 'Windows', value: 'windows' },
  { label: 'Linux', value: 'linux' },
  { label: 'macOS', value: 'macos' }
]
export const brandOptions = ['Chrome', 'Edge', 'Opera', 'Vivaldi']

// 设备核心 / 设备内存的可选值。deviceMemory 按规范上限为 8。
export const coreOptions = ['2', '4', '6', '8', '10', '12', '16', '20', '24', '32']
export const memoryOptions = ['2', '4', '8']

// 给默认选项加标记，避免用户不知道哪个值是默认。
export const withDefaultMark = (value, defaultValue) =>
  value === defaultValue ? `${value}（默认）` : value

// label 与 value 不同名时用这个，按 value 判断是否加标记。
export const withDefaultMarkLabel = (label, value, defaultValue) =>
  value === defaultValue ? `${label}（默认）` : label

export const screens = [
  '1280x720',
  '1280x800',
  '1280x1024',
  '1366x768',
  '1440x900',
  '1536x864',
  '1600x900',
  '1920x1080',
  '1920x1200',
  '2560x1440',
  '3840x2160'
]

export const BASE_URL = 'http://127.0.0.1:10101'
