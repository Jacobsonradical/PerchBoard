package feeds

import (
	"net/url"
	"time"
)

// GeoLocation is a resolved place: coordinates plus a human label.
type GeoLocation struct {
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
	City      string  `json:"city"`
	Region    string  `json:"region"`
	Country   string  `json:"country"`
}

// AutoLocate guesses the user's location from their public IP (ipapi.co, no
// key). This is the fallback when the browser geolocation permission is denied.
// Cached for an hour. Note: from a VPN/datacenter this reflects the server IP.
func AutoLocate() (*GeoLocation, error) {
	v, err := cached("geo:auto", time.Hour, func() (any, error) {
		var raw struct {
			Lat     float64 `json:"latitude"`
			Lon     float64 `json:"longitude"`
			City    string  `json:"city"`
			Region  string  `json:"region"`
			Country string  `json:"country_name"`
		}
		if err := getJSON("https://ipapi.co/json/", &raw); err != nil {
			return nil, err
		}
		return &GeoLocation{
			Latitude: raw.Lat, Longitude: raw.Lon,
			City: raw.City, Region: raw.Region, Country: raw.Country,
		}, nil
	})
	if err != nil {
		return nil, err
	}
	return v.(*GeoLocation), nil
}

// SearchPlace resolves a free-text place name to coordinates using Open-Meteo's
// geocoding API. Used by the weather widget's "set location" box. Cached 1h.
func SearchPlace(name string) ([]GeoLocation, error) {
	key := "geo:search:" + name
	v, err := cached(key, time.Hour, func() (any, error) {
		api := "https://geocoding-api.open-meteo.com/v1/search?count=5&name=" + url.QueryEscape(name)
		var raw struct {
			Results []struct {
				Lat     float64 `json:"latitude"`
				Lon     float64 `json:"longitude"`
				Name    string  `json:"name"`
				Admin1  string  `json:"admin1"`
				Country string  `json:"country"`
			} `json:"results"`
		}
		if err := getJSON(api, &raw); err != nil {
			return nil, err
		}
		out := make([]GeoLocation, 0, len(raw.Results))
		for _, r := range raw.Results {
			out = append(out, GeoLocation{
				Latitude: r.Lat, Longitude: r.Lon,
				City: r.Name, Region: r.Admin1, Country: r.Country,
			})
		}
		return out, nil
	})
	if err != nil {
		return nil, err
	}
	return v.([]GeoLocation), nil
}
