/**
 * 本地文件读写的统一入口。App 里直接用 expo-file-system；
 * H5 没有文件系统，Metro 打 Web 包时会改用同目录的 fs.web.ts（浏览器里用 blob 地址和内存表代替）。
 * 业务代码一律从这里引入 File / Directory / Paths，不直接引 expo-file-system。
 */
export { Directory, File, Paths } from 'expo-file-system';
