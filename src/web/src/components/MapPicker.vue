<template>
  <teleport to="body">
    <div v-show="state.visible" class="map-picker-overlay" @click.self="onClose">
      <div class="map-picker-dialog">
        <div class="map-picker-header">
          <span class="map-picker-title">
            {{ state.readonly ? '查看位置 ' + state.location : '选择位置' }}
          </span>
          <button class="map-picker-close" @click="onClose">
            <el-icon><Close /></el-icon>
          </button>
        </div>
        <div class="map-picker-body">
          <div ref="mapRef" class="map-picker-map"></div>
          <div v-if="!state.readonly" class="map-picker-search">
            <el-input
              v-model="searchQuery"
              placeholder="搜索地点"
              clearable
              @keyup.enter="onSearch"
            >
              <template #append>
                <el-button :icon="Search" @click="onSearch"></el-button>
              </template>
            </el-input>
          </div>
        </div>
      </div>
    </div>
  </teleport>
</template>

<script setup>
import { ref, watch, nextTick, onUnmounted } from 'vue'
import { Search, Close } from '@element-plus/icons-vue'
import { state, resolve } from '@/utils/mapPicker'

const mapRef = ref(null)
const searchQuery = ref('')
let mapInstance = null
let mapMarker = null

const parseLocation = (loc) => {
  if (!loc) return null
  const parts = loc.split(',')
  if (parts.length === 2) {
    const lat = parseFloat(parts[0])
    const lng = parseFloat(parts[1])
    if (!isNaN(lat) && !isNaN(lng)) return { lat, lng }
  }
  return null
}

const reverseGeocode = async (lat, lng) => {
  try {
    const resp = await fetch(
      `https://nominatim.openstreetmap.org/reverse?format=json&accept-language=${navigator.language}&lat=${lat}&lon=${lng}&zoom=10`
    )
    const data = await resp.json()
    return data?.display_name || `${lat}, ${lng}`
  } catch {
    return `${lat}, ${lng}`
  }
}

const initMap = () => {
  const L = window.L
  if (!L || !mapRef.value) return

  const pos = parseLocation(state.location)
  const lat = pos ? pos.lat : 30
  const lng = pos ? pos.lng : 114
  const zoom = pos ? 10 : 3

  if (mapInstance) {
    if (mapMarker) {
      mapMarker.remove()
      mapMarker = null
    }
    mapInstance.setView([lat, lng], zoom)
    mapInstance.invalidateSize()
    if (pos) {
      mapMarker = L.marker([lat, lng]).addTo(mapInstance)
      reverseGeocode(lat, lng).then((desc) => {
        mapMarker.bindPopup(desc).openPopup()
      })
    }
    return
  }

  mapInstance = L.map(mapRef.value, { zoomControl: false }).setView([lat, lng], zoom)
  L.control.zoom({ position: 'topright' }).addTo(mapInstance)
  L.tileLayer('https://{s}.tile.openstreetmap.org/{z}/{x}/{y}.png', {
    attribution: '&copy; OpenStreetMap',
    maxZoom: 18
  }).addTo(mapInstance)

  if (pos) {
    mapMarker = L.marker([lat, lng]).addTo(mapInstance)
    reverseGeocode(lat, lng).then((desc) => {
      mapMarker.bindPopup(desc).openPopup()
    })
  }

  mapInstance.on('click', (e) => {
    if (state.readonly) return
    const { lat: newLat, lng: newLng } = e.latlng
    const loc = newLat.toFixed(3) + ',' + newLng.toFixed(3)
    if (mapMarker) {
      mapMarker.setLatLng(e.latlng)
    } else {
      mapMarker = L.marker(e.latlng).addTo(mapInstance)
    }
    reverseGeocode(newLat, newLng).then((desc) => {
      mapMarker.bindPopup(desc).openPopup()
    })
    resolve(loc)
  })
}

const destroyMap = () => {
  if (mapInstance) {
    mapInstance.remove()
    mapInstance = null
    mapMarker = null
  }
}

const onClose = () => {
  resolve(null)
}

const onSearch = async () => {
  const q = searchQuery.value.trim()
  if (!q || !mapInstance) return
  try {
    const resp = await fetch(
      `https://nominatim.openstreetmap.org/search?format=json&q=${encodeURIComponent(q)}&limit=1`
    )
    const data = await resp.json()
    if (data && data.length > 0) {
      const { lat, lon, display_name } = data[0]
      const latNum = parseFloat(lat)
      const lngNum = parseFloat(lon)
      mapInstance.setView([latNum, lngNum], 12)
      const L = window.L
      if (mapMarker) {
        mapMarker.setLatLng([latNum, lngNum])
      } else {
        mapMarker = L.marker([latNum, lngNum]).addTo(mapInstance)
      }
      if (display_name) {
        mapMarker.bindPopup(display_name).openPopup()
      }
    }
  } catch (e) {
    console.error('[MapPicker] search failed:', e)
  }
}

watch(
  () => state.visible,
  (val) => {
    if (val) {
      searchQuery.value = ''
      if (mapMarker) {
        mapMarker.remove()
        mapMarker = null
      }
      nextTick(() => setTimeout(initMap, 100))
    }
  }
)

onUnmounted(() => {
  destroyMap()
})
</script>

<style>
.map-picker-overlay {
  position: fixed;
  top: 0;
  left: 0;
  right: 0;
  bottom: 0;
  background: rgba(0, 0, 0, 0.5);
  z-index: 3000;
  display: flex;
  align-items: center;
  justify-content: center;
}
.map-picker-dialog {
  background: #fff;
  border-radius: 4px;
  width: min(800px, calc(100vw - 24px));
  max-height: calc(100vh - 24px);
  overflow-y: auto;
  box-shadow: 0 4px 20px rgba(0, 0, 0, 0.15);
}
.map-picker-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 20px 20px 10px;
  border-bottom: 1px solid #dcdfe6;
}
.map-picker-title {
  font-size: 18px;
  color: #303133;
  line-height: 24px;
}
.map-picker-close {
  background: none;
  border: none;
  padding: 0;
  cursor: pointer;
  font-size: 16px;
  color: #909399;
  display: flex;
  align-items: center;
}
.map-picker-close:hover {
  color: #409eff;
}
.map-picker-body {
  position: relative;
  padding: 20px;
}
.map-picker-search {
  position: absolute;
  top: 30px;
  left: 30px;
  right: 30px;
  z-index: 1000;
  width: 280px;
  max-width: calc(100% - 60px);
}
.map-picker-map {
  width: 100%;
  height: min(475px, 60vh);
  min-height: 240px;
  border-radius: 4px;
}

@media (max-width: 767px) {
  .map-picker-header {
    padding: 14px 14px 8px;
  }
  .map-picker-title {
    font-size: 16px;
  }
  .map-picker-body {
    padding: 12px;
  }
  .map-picker-search {
    top: 20px;
    left: 20px;
    right: 20px;
    width: auto;
    max-width: none;
  }
}
</style>
