<script lang="ts" module>
  // Material Symbols Rounded, weight 400 (@material-symbols/svg-400, Apache-2.0). Only the files named here are
  // bundled; each becomes its path data. Add a symbol to the glob before using it.
  const files = import.meta.glob(
    '../../node_modules/@material-symbols/svg-400/rounded/{folder,folder-fill,draft,draft-fill,description,description-fill,picture_as_pdf-fill,image,image-fill,movie-fill,audio_file-fill,folder_zip,folder_zip-fill,table-fill,slideshow-fill,deployed_code-fill,code,upload,upload_file,drive_folder_upload,create_new_folder,check,check_circle-fill,pause,sync,cloud,keep,hard_drive,language,download,delete,delete-fill,restore_from_trash,settings,settings-fill,search,drive_file_rename,more_vert,chevron_left,chevron_right,close,key,computer,logout,add,error,error-fill,grid_view,list,schedule,schedule-fill,history,info,share,link,content_copy,file_copy,content_cut,content_paste,checklist,visibility,lock,mail,arrow_back,arrow_upward,arrow_downward,arrow_drop_down,keyboard_arrow_down,keyboard_arrow_up}.svg',
    { query: '?raw', import: 'default', eager: true },
  ) as Record<string, string>
  const symbols: Record<string, string> = {}
  for (const [file, svg] of Object.entries(files)) symbols[file.slice(file.lastIndexOf('/') + 1, -4)] = svg.match(/ d="([^"]+)"/)![1]

  // The app's own names (used before Material Symbols arrived) → symbol names. Any other name is a symbol itself.
  const alias: Record<string, string> = {
    file: 'draft', text: 'description', pdf: 'picture_as_pdf', video: 'movie', audio: 'audio_file', play: 'play_arrow',
    pin: 'keep', drive: 'hard_drive', globe: 'language', folderPlus: 'create_new_folder', trash: 'delete',
    rename: 'drive_file_rename', more: 'more_vert', chevron: 'chevron_right', restore: 'restore_from_trash', laptop: 'computer',
    plus: 'add', alert: 'error', grid: 'grid_view', clock: 'schedule', folderUp: 'drive_folder_upload',
    copy: 'content_copy', cut: 'content_cut', paste: 'content_paste', archive: 'folder_zip', select: 'checklist', eye: 'visibility',
  }
</script>

<script lang="ts">
  let { name, size = 20, fill = false }: { name: string; size?: number; fill?: boolean } = $props()
  const s = $derived(alias[name] ?? name)
  const d = $derived((fill && symbols[s + '-fill']) || symbols[s] || symbols[s + '-fill'] || symbols.draft)
</script>

<svg width={size} height={size} viewBox="0 -960 960 960" fill="currentColor" aria-hidden="true"><path {d} /></svg>
