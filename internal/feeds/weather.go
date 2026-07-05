package feeds

import (
	"fmt"
	"net/url"
	"time"
)

// Weather is the normalized forecast we send to the frontend. It covers the
// task requirements: current conditions, past + future temperatures, wind,
// rain probability, and sunrise/sunset.
type Weather struct {
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
	Timezone  string  `json:"timezone"`
	Place     string  `json:"place"` // resolved location label (set by the handler)

	Current struct {
		Time          string  `json:"time"`
		Temperature   float64 `json:"temperature"`
		ApparentTemp  float64 `json:"apparentTemperature"`
		Humidity      int     `json:"humidity"`
		WeatherCode   int     `json:"weatherCode"`
		WindSpeed     float64 `json:"windSpeed"`
		Precipitation float64 `json:"precipitation"`
		IsDay         int     `json:"isDay"`
	} `json:"current"`

	// Hourly holds the next ~24h used for the mini temperature/rain trend.
	Hourly []HourPoint `json:"hourly"`

	// Daily holds past + upcoming days (sunrise/sunset, min/max, rain%).
	Daily []DayPoint `json:"daily"`

	Units struct {
		Temperature string `json:"temperature"`
		Wind        string `json:"wind"`
	} `json:"units"`
}

type HourPoint struct {
	Time        string  `json:"time"`
	Temperature float64 `json:"temperature"`
	RainChance  int     `json:"rainChance"`
	WeatherCode int     `json:"weatherCode"`
}

type DayPoint struct {
	Date        string  `json:"date"`
	Min         float64 `json:"min"`
	Max         float64 `json:"max"`
	WeatherCode int     `json:"weatherCode"`
	RainChance  int     `json:"rainChance"`
	WindMax     float64 `json:"windMax"`
	Sunrise     string  `json:"sunrise"`
	Sunset      string  `json:"sunset"`
}

// FetchWeather queries Open-Meteo (free, no API key). tempUnit is "celsius" or
// "fahrenheit"; windUnit is "kmh","mph","ms","kn". Cached 15 minutes per query.
func FetchWeather(lat, lon float64, tempUnit, windUnit string) (*Weather, error) {
	if tempUnit == "" {
		tempUnit = "celsius"
	}
	if windUnit == "" {
		windUnit = "kmh"
	}
	key := fmt.Sprintf("weather:%.3f:%.3f:%s:%s", lat, lon, tempUnit, windUnit)

	v, err := cached(key, 15*time.Minute, func() (any, error) {
		q := url.Values{}
		q.Set("latitude", fmt.Sprintf("%f", lat))
		q.Set("longitude", fmt.Sprintf("%f", lon))
		q.Set("timezone", "auto")
		q.Set("temperature_unit", tempUnit)
		q.Set("wind_speed_unit", windUnit)
		q.Set("past_days", "2")
		q.Set("forecast_days", "7")
		q.Set("current", "temperature_2m,relative_humidity_2m,apparent_temperature,is_day,precipitation,weather_code,wind_speed_10m")
		q.Set("hourly", "temperature_2m,precipitation_probability,weather_code")
		q.Set("daily", "weather_code,temperature_2m_max,temperature_2m_min,sunrise,sunset,precipitation_probability_max,wind_speed_10m_max")

		// Raw mirrors Open-Meteo's array-of-columns layout.
		var raw struct {
			Latitude  float64 `json:"latitude"`
			Longitude float64 `json:"longitude"`
			Timezone  string  `json:"timezone"`
			Current   struct {
				Time         string  `json:"time"`
				Temp         float64 `json:"temperature_2m"`
				Apparent     float64 `json:"apparent_temperature"`
				Humidity     int     `json:"relative_humidity_2m"`
				Code         int     `json:"weather_code"`
				Wind         float64 `json:"wind_speed_10m"`
				Precip       float64 `json:"precipitation"`
				IsDay        int     `json:"is_day"`
			} `json:"current"`
			CurrentUnits struct {
				Temp string `json:"temperature_2m"`
				Wind string `json:"wind_speed_10m"`
			} `json:"current_units"`
			Hourly struct {
				Time     []string  `json:"time"`
				Temp     []float64 `json:"temperature_2m"`
				Rain     []int     `json:"precipitation_probability"`
				Code     []int     `json:"weather_code"`
			} `json:"hourly"`
			Daily struct {
				Time    []string  `json:"time"`
				Code    []int     `json:"weather_code"`
				Max     []float64 `json:"temperature_2m_max"`
				Min     []float64 `json:"temperature_2m_min"`
				Sunrise []string  `json:"sunrise"`
				Sunset  []string  `json:"sunset"`
				Rain    []int     `json:"precipitation_probability_max"`
				WindMax []float64 `json:"wind_speed_10m_max"`
			} `json:"daily"`
		}
		api := "https://api.open-meteo.com/v1/forecast?" + q.Encode()
		if err := getJSON(api, &raw); err != nil {
			return nil, err
		}

		w := &Weather{Latitude: raw.Latitude, Longitude: raw.Longitude, Timezone: raw.Timezone}
		w.Current.Time = raw.Current.Time
		w.Current.Temperature = raw.Current.Temp
		w.Current.ApparentTemp = raw.Current.Apparent
		w.Current.Humidity = raw.Current.Humidity
		w.Current.WeatherCode = raw.Current.Code
		w.Current.WindSpeed = raw.Current.Wind
		w.Current.Precipitation = raw.Current.Precip
		w.Current.IsDay = raw.Current.IsDay
		w.Units.Temperature = raw.CurrentUnits.Temp
		w.Units.Wind = raw.CurrentUnits.Wind

		// Transpose the hourly columns; keep only from "now" forward, 24 points.
		nowIdx := 0
		for i, t := range raw.Hourly.Time {
			if t >= raw.Current.Time {
				nowIdx = i
				break
			}
		}
		for i := nowIdx; i < len(raw.Hourly.Time) && len(w.Hourly) < 24; i++ {
			w.Hourly = append(w.Hourly, HourPoint{
				Time:        raw.Hourly.Time[i],
				Temperature: at(raw.Hourly.Temp, i),
				RainChance:  atInt(raw.Hourly.Rain, i),
				WeatherCode: atInt(raw.Hourly.Code, i),
			})
		}

		for i := range raw.Daily.Time {
			w.Daily = append(w.Daily, DayPoint{
				Date:        raw.Daily.Time[i],
				Min:         at(raw.Daily.Min, i),
				Max:         at(raw.Daily.Max, i),
				WeatherCode: atInt(raw.Daily.Code, i),
				RainChance:  atInt(raw.Daily.Rain, i),
				WindMax:     at(raw.Daily.WindMax, i),
				Sunrise:     atStr(raw.Daily.Sunrise, i),
				Sunset:      atStr(raw.Daily.Sunset, i),
			})
		}
		return w, nil
	})
	if err != nil {
		return nil, err
	}
	return v.(*Weather), nil
}

// small bounds-safe slice accessors keep the transpose loops readable.
func at(s []float64, i int) float64 { if i < len(s) { return s[i] }; return 0 }
func atInt(s []int, i int) int      { if i < len(s) { return s[i] }; return 0 }
func atStr(s []string, i int) string { if i < len(s) { return s[i] }; return "" }
