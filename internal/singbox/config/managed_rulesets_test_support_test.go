package singboxconfig

func setGeositeRuSmartDownloaderForTest(fn geositeRuSmartDownloader) func() {
	previous := downloadGeositeRuSmart
	if fn == nil {
		downloadGeositeRuSmart = downloadGeositeRuSmartHTTP
	} else {
		downloadGeositeRuSmart = fn
	}
	return func() { downloadGeositeRuSmart = previous }
}
