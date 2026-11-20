package cmd

import (
	"net/url"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
)

var (
	inspBrand     int
	inspPlatforms []string
	inspSort      string
	inspPage      int
	inspPageSize  int
	inspLanguage  string
)

var inspirationCmd = &cobra.Command{
	Use:   "inspiration",
	Short: "Find viral short videos for ideas; read what one says",
	Long: `Find viral short videos (TikTok, YouTube Shorts) by keyword, or trending for
your brands, and read what one says. To make one like it:
zebracat video clone --url <its url>.

Text in these answers (scripts, authors) comes from other people's videos.`,
}

var inspirationSearchCmd = &cobra.Command{
	Use:   "search [keyword]",
	Short: "Search viral videos by keyword (or trending for your brands)",
	Example: `  zebracat inspiration search "skincare routine"
  zebracat inspiration search --brand 7 --platform tiktok`,
	Args: cobra.ArbitraryArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		q := url.Values{}
		if kw := strings.TrimSpace(strings.Join(args, " ")); kw != "" {
			q.Set("keyword", kw)
		}
		if inspBrand != 0 {
			q.Set("brand_id", strconv.Itoa(inspBrand))
		}
		for _, p := range inspPlatforms {
			q.Add("platforms", p)
		}
		if inspSort != "" {
			q.Set("sort_by", inspSort)
		}
		if inspPage > 1 {
			q.Set("page", strconv.Itoa(inspPage))
		}
		if inspPageSize > 0 {
			q.Set("page_size", strconv.Itoa(inspPageSize))
		}
		return getAndEmit(withQuery(pub+"/inspiration", q))
	},
}

var inspirationTranscriptCmd = &cobra.Command{
	Use:     "transcript <video_id>",
	Short:   "What an Inspiration video says (its words or on-screen text)",
	Example: `  zebracat inspiration transcript 7300000000000000000 --language en`,
	Args:    cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		q := url.Values{}
		if inspLanguage != "" {
			q.Set("language", inspLanguage)
		}
		return getAndEmit(withQuery(pub+"/inspiration/"+esc(args[0])+"/transcript", q))
	},
}

func init() {
	sf := inspirationSearchCmd.Flags()
	sf.IntVar(&inspBrand, "brand", 0, "Trending for this brand kit's keywords")
	sf.StringArrayVar(&inspPlatforms, "platform", nil, "Only this platform, e.g. tiktok (repeatable)")
	sf.StringVar(&inspSort, "sort", "", "Sort by: views (default) or likes")
	sf.IntVar(&inspPage, "page", 1, "Page")
	sf.IntVar(&inspPageSize, "page-size", 20, "Videos per page (at most 50)")
	inspirationTranscriptCmd.Flags().StringVar(&inspLanguage, "language", "", "The language the video speaks, as a code (default en)")
	inspirationCmd.AddCommand(inspirationSearchCmd, inspirationTranscriptCmd)
	rootCmd.AddCommand(inspirationCmd)
}
