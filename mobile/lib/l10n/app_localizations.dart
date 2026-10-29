import 'dart:async';

import 'package:flutter/foundation.dart';
import 'package:flutter/widgets.dart';
import 'package:flutter_localizations/flutter_localizations.dart';
import 'package:intl/intl.dart' as intl;

import 'app_localizations_en.dart';
import 'app_localizations_es.dart';
import 'app_localizations_hi.dart';

// ignore_for_file: type=lint

/// Callers can lookup localized strings with an instance of AppLocalizations
/// returned by `AppLocalizations.of(context)`.
///
/// Applications need to include `AppLocalizations.delegate()` in their app's
/// `localizationDelegates` list, and the locales they support in the app's
/// `supportedLocales` list. For example:
///
/// ```dart
/// import 'l10n/app_localizations.dart';
///
/// return MaterialApp(
///   localizationsDelegates: AppLocalizations.localizationsDelegates,
///   supportedLocales: AppLocalizations.supportedLocales,
///   home: MyApplicationHome(),
/// );
/// ```
///
/// ## Update pubspec.yaml
///
/// Please make sure to update your pubspec.yaml to include the following
/// packages:
///
/// ```yaml
/// dependencies:
///   # Internationalization support.
///   flutter_localizations:
///     sdk: flutter
///   intl: any # Use the pinned version from flutter_localizations
///
///   # Rest of dependencies
/// ```
///
/// ## iOS Applications
///
/// iOS applications define key application metadata, including supported
/// locales, in an Info.plist file that is built into the application bundle.
/// To configure the locales supported by your app, you’ll need to edit this
/// file.
///
/// First, open your project’s ios/Runner.xcworkspace Xcode workspace file.
/// Then, in the Project Navigator, open the Info.plist file under the Runner
/// project’s Runner folder.
///
/// Next, select the Information Property List item, select Add Item from the
/// Editor menu, then select Localizations from the pop-up menu.
///
/// Select and expand the newly-created Localizations item then, for each
/// locale your application supports, add a new item and select the locale
/// you wish to add from the pop-up menu in the Value field. This list should
/// be consistent with the languages listed in the AppLocalizations.supportedLocales
/// property.
abstract class AppLocalizations {
  AppLocalizations(String locale)
    : localeName = intl.Intl.canonicalizedLocale(locale.toString());

  final String localeName;

  static AppLocalizations? of(BuildContext context) {
    return Localizations.of<AppLocalizations>(context, AppLocalizations);
  }

  static const LocalizationsDelegate<AppLocalizations> delegate =
      _AppLocalizationsDelegate();

  /// A list of this localizations delegate along with the default localizations
  /// delegates.
  ///
  /// Returns a list of localizations delegates containing this delegate along with
  /// GlobalMaterialLocalizations.delegate, GlobalCupertinoLocalizations.delegate,
  /// and GlobalWidgetsLocalizations.delegate.
  ///
  /// Additional delegates can be added by appending to this list in
  /// MaterialApp. This list does not have to be used at all if a custom list
  /// of delegates is preferred or required.
  static const List<LocalizationsDelegate<dynamic>> localizationsDelegates =
      <LocalizationsDelegate<dynamic>>[
        delegate,
        GlobalMaterialLocalizations.delegate,
        GlobalCupertinoLocalizations.delegate,
        GlobalWidgetsLocalizations.delegate,
      ];

  /// A list of this localizations delegate's supported locales.
  static const List<Locale> supportedLocales = <Locale>[
    Locale('en'),
    Locale('es'),
    Locale('hi'),
  ];

  /// No description provided for @navDiscover.
  ///
  /// In en, this message translates to:
  /// **'Discover'**
  String get navDiscover;

  /// No description provided for @navHotel.
  ///
  /// In en, this message translates to:
  /// **'Hotel'**
  String get navHotel;

  /// No description provided for @navGroups.
  ///
  /// In en, this message translates to:
  /// **'Groups'**
  String get navGroups;

  /// No description provided for @navChats.
  ///
  /// In en, this message translates to:
  /// **'Chats'**
  String get navChats;

  /// No description provided for @navProfile.
  ///
  /// In en, this message translates to:
  /// **'Profile'**
  String get navProfile;

  /// No description provided for @appearanceTitle.
  ///
  /// In en, this message translates to:
  /// **'Appearance'**
  String get appearanceTitle;

  /// No description provided for @appearanceTheme.
  ///
  /// In en, this message translates to:
  /// **'Theme'**
  String get appearanceTheme;

  /// No description provided for @appearanceMatchPhone.
  ///
  /// In en, this message translates to:
  /// **'Match phone'**
  String get appearanceMatchPhone;

  /// No description provided for @appearanceLight.
  ///
  /// In en, this message translates to:
  /// **'Light'**
  String get appearanceLight;

  /// No description provided for @appearanceDark.
  ///
  /// In en, this message translates to:
  /// **'Dark'**
  String get appearanceDark;

  /// No description provided for @appearanceTextSize.
  ///
  /// In en, this message translates to:
  /// **'Text size'**
  String get appearanceTextSize;

  /// No description provided for @textSizeDefault.
  ///
  /// In en, this message translates to:
  /// **'Default'**
  String get textSizeDefault;

  /// No description provided for @textSizeLarge.
  ///
  /// In en, this message translates to:
  /// **'Large'**
  String get textSizeLarge;

  /// No description provided for @textSizeLarger.
  ///
  /// In en, this message translates to:
  /// **'Larger'**
  String get textSizeLarger;

  /// No description provided for @textSizeLargest.
  ///
  /// In en, this message translates to:
  /// **'Largest'**
  String get textSizeLargest;

  /// No description provided for @appearanceReduceMotion.
  ///
  /// In en, this message translates to:
  /// **'Reduce motion'**
  String get appearanceReduceMotion;

  /// No description provided for @appearanceReduceMotionHint.
  ///
  /// In en, this message translates to:
  /// **'Less movement: no flying gifts, floating cards or springy swipes'**
  String get appearanceReduceMotionHint;

  /// No description provided for @appearanceLanguage.
  ///
  /// In en, this message translates to:
  /// **'Language'**
  String get appearanceLanguage;

  /// No description provided for @appearanceLanguageSystem.
  ///
  /// In en, this message translates to:
  /// **'Match phone'**
  String get appearanceLanguageSystem;

  /// No description provided for @appearanceSavedHere.
  ///
  /// In en, this message translates to:
  /// **'These choices are saved on this device only.'**
  String get appearanceSavedHere;

  /// No description provided for @brandTagline.
  ///
  /// In en, this message translates to:
  /// **'Be seen only as much as you choose.'**
  String get brandTagline;

  /// No description provided for @loginEmail.
  ///
  /// In en, this message translates to:
  /// **'Email'**
  String get loginEmail;

  /// No description provided for @loginPassword.
  ///
  /// In en, this message translates to:
  /// **'Password'**
  String get loginPassword;

  /// No description provided for @loginSignIn.
  ///
  /// In en, this message translates to:
  /// **'Sign in'**
  String get loginSignIn;

  /// No description provided for @loginForgot.
  ///
  /// In en, this message translates to:
  /// **'Forgot password?'**
  String get loginForgot;

  /// No description provided for @loginCreate.
  ///
  /// In en, this message translates to:
  /// **'Create an account'**
  String get loginCreate;

  /// No description provided for @discoverTitle.
  ///
  /// In en, this message translates to:
  /// **'Discover'**
  String get discoverTitle;

  /// No description provided for @tabSwipe.
  ///
  /// In en, this message translates to:
  /// **'Swipe'**
  String get tabSwipe;

  /// No description provided for @tabTopPicks.
  ///
  /// In en, this message translates to:
  /// **'Top picks'**
  String get tabTopPicks;

  /// No description provided for @tabNearby.
  ///
  /// In en, this message translates to:
  /// **'Nearby'**
  String get tabNearby;

  /// No description provided for @tabLikesYou.
  ///
  /// In en, this message translates to:
  /// **'Likes you'**
  String get tabLikesYou;

  /// No description provided for @profileAboutYou.
  ///
  /// In en, this message translates to:
  /// **'About you'**
  String get profileAboutYou;

  /// No description provided for @profilePrompts.
  ///
  /// In en, this message translates to:
  /// **'Your prompts'**
  String get profilePrompts;

  /// No description provided for @profileSafety.
  ///
  /// In en, this message translates to:
  /// **'Safety center'**
  String get profileSafety;

  /// No description provided for @profileInvest.
  ///
  /// In en, this message translates to:
  /// **'Invest in ConnectMe'**
  String get profileInvest;

  /// No description provided for @profileQuiz.
  ///
  /// In en, this message translates to:
  /// **'Compatibility quiz'**
  String get profileQuiz;

  /// No description provided for @profileLevels.
  ///
  /// In en, this message translates to:
  /// **'Levels and leaderboards'**
  String get profileLevels;

  /// No description provided for @profileFollowing.
  ///
  /// In en, this message translates to:
  /// **'Following'**
  String get profileFollowing;

  /// No description provided for @profileNotifications.
  ///
  /// In en, this message translates to:
  /// **'Notifications'**
  String get profileNotifications;

  /// No description provided for @profileSignOut.
  ///
  /// In en, this message translates to:
  /// **'Sign out'**
  String get profileSignOut;

  /// No description provided for @regTitle.
  ///
  /// In en, this message translates to:
  /// **'Create account'**
  String get regTitle;

  /// No description provided for @regPhone.
  ///
  /// In en, this message translates to:
  /// **'Phone (optional, +14155550123)'**
  String get regPhone;

  /// No description provided for @regDob.
  ///
  /// In en, this message translates to:
  /// **'Date of birth'**
  String get regDob;

  /// No description provided for @regAdults.
  ///
  /// In en, this message translates to:
  /// **'ConnectMe is for adults only.'**
  String get regAdults;

  /// No description provided for @forgotTitle.
  ///
  /// In en, this message translates to:
  /// **'Forgot password'**
  String get forgotTitle;

  /// No description provided for @forgotSend.
  ///
  /// In en, this message translates to:
  /// **'Send reset email'**
  String get forgotSend;

  /// No description provided for @chatsTitle.
  ///
  /// In en, this message translates to:
  /// **'Chats'**
  String get chatsTitle;

  /// No description provided for @sAddACaption.
  ///
  /// In en, this message translates to:
  /// **'Add a caption'**
  String get sAddACaption;

  /// No description provided for @sAddASound.
  ///
  /// In en, this message translates to:
  /// **'Add a sound'**
  String get sAddASound;

  /// No description provided for @sAddSomeoneYouTrust.
  ///
  /// In en, this message translates to:
  /// **'Add someone you trust'**
  String get sAddSomeoneYouTrust;

  /// No description provided for @sAgree.
  ///
  /// In en, this message translates to:
  /// **'Agree'**
  String get sAgree;

  /// No description provided for @sAnonymous.
  ///
  /// In en, this message translates to:
  /// **'Anonymous'**
  String get sAnonymous;

  /// No description provided for @sAny.
  ///
  /// In en, this message translates to:
  /// **'Any'**
  String get sAny;

  /// No description provided for @sAskWingmanForAReply.
  ///
  /// In en, this message translates to:
  /// **'Ask Wingman for a reply'**
  String get sAskWingmanForAReply;

  /// No description provided for @sBack.
  ///
  /// In en, this message translates to:
  /// **'Back'**
  String get sBack;

  /// No description provided for @sBlock.
  ///
  /// In en, this message translates to:
  /// **'Block'**
  String get sBlock;

  /// No description provided for @sCalls.
  ///
  /// In en, this message translates to:
  /// **'Calls'**
  String get sCalls;

  /// No description provided for @sCancel.
  ///
  /// In en, this message translates to:
  /// **'Cancel'**
  String get sCancel;

  /// No description provided for @sCancelPlan.
  ///
  /// In en, this message translates to:
  /// **'Cancel plan'**
  String get sCancelPlan;

  /// No description provided for @sCancelSubscription.
  ///
  /// In en, this message translates to:
  /// **'Cancel subscription'**
  String get sCancelSubscription;

  /// No description provided for @sCancelSubscription2.
  ///
  /// In en, this message translates to:
  /// **'Cancel subscription?'**
  String get sCancelSubscription2;

  /// No description provided for @sChange.
  ///
  /// In en, this message translates to:
  /// **'Change'**
  String get sChange;

  /// No description provided for @sChangePassword.
  ///
  /// In en, this message translates to:
  /// **'Change password'**
  String get sChangePassword;

  /// No description provided for @sCheckIn.
  ///
  /// In en, this message translates to:
  /// **'Check in'**
  String get sCheckIn;

  /// No description provided for @sCheckOut.
  ///
  /// In en, this message translates to:
  /// **'Check out'**
  String get sCheckOut;

  /// No description provided for @sChooseFromYourLibrary.
  ///
  /// In en, this message translates to:
  /// **'Choose from your library'**
  String get sChooseFromYourLibrary;

  /// No description provided for @sClear.
  ///
  /// In en, this message translates to:
  /// **'Clear'**
  String get sClear;

  /// No description provided for @sClose.
  ///
  /// In en, this message translates to:
  /// **'Close'**
  String get sClose;

  /// No description provided for @sCoins.
  ///
  /// In en, this message translates to:
  /// **'Coins'**
  String get sCoins;

  /// No description provided for @sConfirm.
  ///
  /// In en, this message translates to:
  /// **'Confirm'**
  String get sConfirm;

  /// No description provided for @sConfirmed.
  ///
  /// In en, this message translates to:
  /// **'Confirmed'**
  String get sConfirmed;

  /// No description provided for @sCopy.
  ///
  /// In en, this message translates to:
  /// **'Copy'**
  String get sCopy;

  /// No description provided for @sCoupleQuiz.
  ///
  /// In en, this message translates to:
  /// **'Couple quiz'**
  String get sCoupleQuiz;

  /// No description provided for @sCreateGroup.
  ///
  /// In en, this message translates to:
  /// **'Create group'**
  String get sCreateGroup;

  /// No description provided for @sDare.
  ///
  /// In en, this message translates to:
  /// **'Dare'**
  String get sDare;

  /// No description provided for @sDateIdeas.
  ///
  /// In en, this message translates to:
  /// **'Date ideas'**
  String get sDateIdeas;

  /// No description provided for @sDelete.
  ///
  /// In en, this message translates to:
  /// **'Delete'**
  String get sDelete;

  /// No description provided for @sDeleteConversation.
  ///
  /// In en, this message translates to:
  /// **'Delete conversation'**
  String get sDeleteConversation;

  /// No description provided for @sDeleteYourAccount.
  ///
  /// In en, this message translates to:
  /// **'Delete your account?'**
  String get sDeleteYourAccount;

  /// No description provided for @sDisappearingMessages.
  ///
  /// In en, this message translates to:
  /// **'Disappearing messages'**
  String get sDisappearingMessages;

  /// No description provided for @sDone.
  ///
  /// In en, this message translates to:
  /// **'Done'**
  String get sDone;

  /// No description provided for @sDoubleDate2.
  ///
  /// In en, this message translates to:
  /// **'Double date (2)'**
  String get sDoubleDate2;

  /// No description provided for @sDownloadMyData.
  ///
  /// In en, this message translates to:
  /// **'Download my data'**
  String get sDownloadMyData;

  /// No description provided for @sEditProfile.
  ///
  /// In en, this message translates to:
  /// **'Edit profile'**
  String get sEditProfile;

  /// No description provided for @sEndGame.
  ///
  /// In en, this message translates to:
  /// **'End game'**
  String get sEndGame;

  /// No description provided for @sEveryone.
  ///
  /// In en, this message translates to:
  /// **'Everyone'**
  String get sEveryone;

  /// No description provided for @sExplore.
  ///
  /// In en, this message translates to:
  /// **'Explore'**
  String get sExplore;

  /// No description provided for @sFindARoom.
  ///
  /// In en, this message translates to:
  /// **'Find a room'**
  String get sFindARoom;

  /// No description provided for @sFollow.
  ///
  /// In en, this message translates to:
  /// **'Follow'**
  String get sFollow;

  /// No description provided for @sForums.
  ///
  /// In en, this message translates to:
  /// **'Forums'**
  String get sForums;

  /// No description provided for @sFrom.
  ///
  /// In en, this message translates to:
  /// **'From'**
  String get sFrom;

  /// No description provided for @sGetMore.
  ///
  /// In en, this message translates to:
  /// **'Get more'**
  String get sGetMore;

  /// No description provided for @sGiftsReceived.
  ///
  /// In en, this message translates to:
  /// **'Gifts received'**
  String get sGiftsReceived;

  /// No description provided for @sGroup.
  ///
  /// In en, this message translates to:
  /// **'Group'**
  String get sGroup;

  /// No description provided for @sGroupDates.
  ///
  /// In en, this message translates to:
  /// **'Group dates'**
  String get sGroupDates;

  /// No description provided for @sGroupsNearby.
  ///
  /// In en, this message translates to:
  /// **'Groups nearby'**
  String get sGroupsNearby;

  /// No description provided for @sHotelMode.
  ///
  /// In en, this message translates to:
  /// **'Hotel mode'**
  String get sHotelMode;

  /// No description provided for @sIAgree.
  ///
  /// In en, this message translates to:
  /// **'I agree'**
  String get sIAgree;

  /// No description provided for @sIAmSafe.
  ///
  /// In en, this message translates to:
  /// **'I am safe'**
  String get sIAmSafe;

  /// No description provided for @sIcebreakerIdeas.
  ///
  /// In en, this message translates to:
  /// **'Icebreaker ideas'**
  String get sIcebreakerIdeas;

  /// No description provided for @sIncognito.
  ///
  /// In en, this message translates to:
  /// **'Incognito'**
  String get sIncognito;

  /// No description provided for @sInvisibleMode.
  ///
  /// In en, this message translates to:
  /// **'Invisible mode'**
  String get sInvisibleMode;

  /// No description provided for @sInviteAsCoHost.
  ///
  /// In en, this message translates to:
  /// **'Invite as co-host'**
  String get sInviteAsCoHost;

  /// No description provided for @sJoin.
  ///
  /// In en, this message translates to:
  /// **'Join'**
  String get sJoin;

  /// No description provided for @sKeep.
  ///
  /// In en, this message translates to:
  /// **'Keep'**
  String get sKeep;

  /// No description provided for @sKeepChatting.
  ///
  /// In en, this message translates to:
  /// **'Keep chatting'**
  String get sKeepChatting;

  /// No description provided for @sLater.
  ///
  /// In en, this message translates to:
  /// **'Later'**
  String get sLater;

  /// No description provided for @sLeave.
  ///
  /// In en, this message translates to:
  /// **'Leave'**
  String get sLeave;

  /// No description provided for @sLeaveTheRoom.
  ///
  /// In en, this message translates to:
  /// **'Leave the room'**
  String get sLeaveTheRoom;

  /// No description provided for @sLevels.
  ///
  /// In en, this message translates to:
  /// **'Levels'**
  String get sLevels;

  /// No description provided for @sMan.
  ///
  /// In en, this message translates to:
  /// **'Man'**
  String get sMan;

  /// No description provided for @sMaskedTruth.
  ///
  /// In en, this message translates to:
  /// **'Masked Truth'**
  String get sMaskedTruth;

  /// No description provided for @sMatched.
  ///
  /// In en, this message translates to:
  /// **'Matched'**
  String get sMatched;

  /// No description provided for @sMatches.
  ///
  /// In en, this message translates to:
  /// **'Matches'**
  String get sMatches;

  /// No description provided for @sMen.
  ///
  /// In en, this message translates to:
  /// **'Men'**
  String get sMen;

  /// No description provided for @sMessages.
  ///
  /// In en, this message translates to:
  /// **'Messages'**
  String get sMessages;

  /// No description provided for @sMostLikelyTo.
  ///
  /// In en, this message translates to:
  /// **'Most likely to'**
  String get sMostLikelyTo;

  /// No description provided for @sNearbyGroups.
  ///
  /// In en, this message translates to:
  /// **'Nearby groups'**
  String get sNearbyGroups;

  /// No description provided for @sNewGroup.
  ///
  /// In en, this message translates to:
  /// **'New group'**
  String get sNewGroup;

  /// No description provided for @sNewThread.
  ///
  /// In en, this message translates to:
  /// **'New thread'**
  String get sNewThread;

  /// No description provided for @sNextPrompt.
  ///
  /// In en, this message translates to:
  /// **'Next prompt'**
  String get sNextPrompt;

  /// No description provided for @sNextQuestion.
  ///
  /// In en, this message translates to:
  /// **'Next question'**
  String get sNextQuestion;

  /// No description provided for @sNoNotificationsInThisWindow.
  ///
  /// In en, this message translates to:
  /// **'No notifications in this window, except incoming calls'**
  String get sNoNotificationsInThisWindow;

  /// No description provided for @sNoPlansRightNow.
  ///
  /// In en, this message translates to:
  /// **'No plans right now.'**
  String get sNoPlansRightNow;

  /// No description provided for @sNoSound.
  ///
  /// In en, this message translates to:
  /// **'No sound'**
  String get sNoSound;

  /// No description provided for @sNonBinary.
  ///
  /// In en, this message translates to:
  /// **'Non-binary'**
  String get sNonBinary;

  /// No description provided for @sNotNow.
  ///
  /// In en, this message translates to:
  /// **'Not now'**
  String get sNotNow;

  /// No description provided for @sOpen.
  ///
  /// In en, this message translates to:
  /// **'Open'**
  String get sOpen;

  /// No description provided for @sOpenChat.
  ///
  /// In en, this message translates to:
  /// **'Open chat'**
  String get sOpenChat;

  /// No description provided for @sOther.
  ///
  /// In en, this message translates to:
  /// **'Other'**
  String get sOther;

  /// No description provided for @sPlayAGame.
  ///
  /// In en, this message translates to:
  /// **'Play a game'**
  String get sPlayAGame;

  /// No description provided for @sPlayAgain.
  ///
  /// In en, this message translates to:
  /// **'Play again'**
  String get sPlayAgain;

  /// No description provided for @sPost.
  ///
  /// In en, this message translates to:
  /// **'Post'**
  String get sPost;

  /// No description provided for @sPostAVideo.
  ///
  /// In en, this message translates to:
  /// **'Post a video'**
  String get sPostAVideo;

  /// No description provided for @sPostThread.
  ///
  /// In en, this message translates to:
  /// **'Post thread'**
  String get sPostThread;

  /// No description provided for @sPropose.
  ///
  /// In en, this message translates to:
  /// **'Propose'**
  String get sPropose;

  /// No description provided for @sProposeADate.
  ///
  /// In en, this message translates to:
  /// **'Propose a date'**
  String get sProposeADate;

  /// No description provided for @sQuietHours.
  ///
  /// In en, this message translates to:
  /// **'Quiet hours'**
  String get sQuietHours;

  /// No description provided for @sRecord.
  ///
  /// In en, this message translates to:
  /// **'Record'**
  String get sRecord;

  /// No description provided for @sRecordAVideo.
  ///
  /// In en, this message translates to:
  /// **'Record a video'**
  String get sRecordAVideo;

  /// No description provided for @sRecordAVoiceIntro.
  ///
  /// In en, this message translates to:
  /// **'Record a voice intro'**
  String get sRecordAVoiceIntro;

  /// No description provided for @sRecordAgain.
  ///
  /// In en, this message translates to:
  /// **'Record again'**
  String get sRecordAgain;

  /// No description provided for @sRefresh.
  ///
  /// In en, this message translates to:
  /// **'Refresh'**
  String get sRefresh;

  /// No description provided for @sRemove.
  ///
  /// In en, this message translates to:
  /// **'Remove'**
  String get sRemove;

  /// No description provided for @sRepliesToYourPosts.
  ///
  /// In en, this message translates to:
  /// **'Replies to your posts'**
  String get sRepliesToYourPosts;

  /// No description provided for @sReply.
  ///
  /// In en, this message translates to:
  /// **'Reply'**
  String get sReply;

  /// No description provided for @sReport.
  ///
  /// In en, this message translates to:
  /// **'Report'**
  String get sReport;

  /// No description provided for @sReportThisPerson.
  ///
  /// In en, this message translates to:
  /// **'Report this person'**
  String get sReportThisPerson;

  /// No description provided for @sReportThisPost.
  ///
  /// In en, this message translates to:
  /// **'Report this post'**
  String get sReportThisPost;

  /// No description provided for @sReportThisVideo.
  ///
  /// In en, this message translates to:
  /// **'Report this video'**
  String get sReportThisVideo;

  /// No description provided for @sRevealMyself.
  ///
  /// In en, this message translates to:
  /// **'Reveal myself'**
  String get sRevealMyself;

  /// No description provided for @sReview.
  ///
  /// In en, this message translates to:
  /// **'Review'**
  String get sReview;

  /// No description provided for @sSayHi.
  ///
  /// In en, this message translates to:
  /// **'Say hi'**
  String get sSayHi;

  /// No description provided for @sSearchInChat.
  ///
  /// In en, this message translates to:
  /// **'Search in chat'**
  String get sSearchInChat;

  /// No description provided for @sSendAGift.
  ///
  /// In en, this message translates to:
  /// **'Send a gift'**
  String get sSendAGift;

  /// No description provided for @sSendLike.
  ///
  /// In en, this message translates to:
  /// **'Send like'**
  String get sSendLike;

  /// No description provided for @sSendPlan.
  ///
  /// In en, this message translates to:
  /// **'Send plan'**
  String get sSendPlan;

  /// No description provided for @sSendToChat.
  ///
  /// In en, this message translates to:
  /// **'Send to chat'**
  String get sSendToChat;

  /// No description provided for @sShareADatePlan.
  ///
  /// In en, this message translates to:
  /// **'Share a date plan'**
  String get sShareADatePlan;

  /// No description provided for @sShareMyDatePlan.
  ///
  /// In en, this message translates to:
  /// **'Share my date plan'**
  String get sShareMyDatePlan;

  /// No description provided for @sSignInToPledge.
  ///
  /// In en, this message translates to:
  /// **'Sign in to pledge'**
  String get sSignInToPledge;

  /// No description provided for @sSignOutOfAllDevices.
  ///
  /// In en, this message translates to:
  /// **'Sign out of all devices'**
  String get sSignOutOfAllDevices;

  /// No description provided for @sSkip.
  ///
  /// In en, this message translates to:
  /// **'Skip'**
  String get sSkip;

  /// No description provided for @sSkipForNow.
  ///
  /// In en, this message translates to:
  /// **'Skip for now'**
  String get sSkipForNow;

  /// No description provided for @sStartAGroup.
  ///
  /// In en, this message translates to:
  /// **'Start a group'**
  String get sStartAGroup;

  /// No description provided for @sStartARound.
  ///
  /// In en, this message translates to:
  /// **'Start a round'**
  String get sStartARound;

  /// No description provided for @sStartAThread.
  ///
  /// In en, this message translates to:
  /// **'Start a thread'**
  String get sStartAThread;

  /// No description provided for @sStartTheGame.
  ///
  /// In en, this message translates to:
  /// **'Start the game'**
  String get sStartTheGame;

  /// No description provided for @sStay.
  ///
  /// In en, this message translates to:
  /// **'Stay'**
  String get sStay;

  /// No description provided for @sStopLooking.
  ///
  /// In en, this message translates to:
  /// **'Stop looking'**
  String get sStopLooking;

  /// No description provided for @sSubscription.
  ///
  /// In en, this message translates to:
  /// **'Subscription'**
  String get sSubscription;

  /// No description provided for @sSwapVoices.
  ///
  /// In en, this message translates to:
  /// **'Swap voices'**
  String get sSwapVoices;

  /// No description provided for @sTakeOver.
  ///
  /// In en, this message translates to:
  /// **'Take over'**
  String get sTakeOver;

  /// No description provided for @sThread.
  ///
  /// In en, this message translates to:
  /// **'Thread'**
  String get sThread;

  /// No description provided for @sTrustTheNewKey.
  ///
  /// In en, this message translates to:
  /// **'Trust the new key'**
  String get sTrustTheNewKey;

  /// No description provided for @sTrustedContact.
  ///
  /// In en, this message translates to:
  /// **'Trusted contact'**
  String get sTrustedContact;

  /// No description provided for @sTruth.
  ///
  /// In en, this message translates to:
  /// **'Truth'**
  String get sTruth;

  /// No description provided for @sTruthOrDare.
  ///
  /// In en, this message translates to:
  /// **'Truth or Dare'**
  String get sTruthOrDare;

  /// No description provided for @sTruthOrDare2.
  ///
  /// In en, this message translates to:
  /// **'Truth or dare'**
  String get sTruthOrDare2;

  /// No description provided for @sTryAgain.
  ///
  /// In en, this message translates to:
  /// **'Try again'**
  String get sTryAgain;

  /// No description provided for @sTurnOnNotifications.
  ///
  /// In en, this message translates to:
  /// **'Turn on notifications'**
  String get sTurnOnNotifications;

  /// No description provided for @sUnmatch.
  ///
  /// In en, this message translates to:
  /// **'Unmatch'**
  String get sUnmatch;

  /// No description provided for @sUnsubscribe.
  ///
  /// In en, this message translates to:
  /// **'Unsubscribe'**
  String get sUnsubscribe;

  /// No description provided for @sUntil.
  ///
  /// In en, this message translates to:
  /// **'Until'**
  String get sUntil;

  /// No description provided for @sUpdateMyLocation.
  ///
  /// In en, this message translates to:
  /// **'Update my location'**
  String get sUpdateMyLocation;

  /// No description provided for @sVerifySecurity.
  ///
  /// In en, this message translates to:
  /// **'Verify security'**
  String get sVerifySecurity;

  /// No description provided for @sVerifyWithId.
  ///
  /// In en, this message translates to:
  /// **'Verify with ID'**
  String get sVerifyWithId;

  /// No description provided for @sVideos.
  ///
  /// In en, this message translates to:
  /// **'Videos'**
  String get sVideos;

  /// No description provided for @sWaiting.
  ///
  /// In en, this message translates to:
  /// **'Waiting'**
  String get sWaiting;

  /// No description provided for @sWeeklySummary.
  ///
  /// In en, this message translates to:
  /// **'Weekly summary'**
  String get sWeeklySummary;

  /// No description provided for @sWelcome.
  ///
  /// In en, this message translates to:
  /// **'Welcome'**
  String get sWelcome;

  /// No description provided for @sWhen.
  ///
  /// In en, this message translates to:
  /// **'When'**
  String get sWhen;

  /// No description provided for @sWoman.
  ///
  /// In en, this message translates to:
  /// **'Woman'**
  String get sWoman;

  /// No description provided for @sWomen.
  ///
  /// In en, this message translates to:
  /// **'Women'**
  String get sWomen;

  /// No description provided for @sWouldYouRather.
  ///
  /// In en, this message translates to:
  /// **'Would you rather'**
  String get sWouldYouRather;

  /// No description provided for @sYourData.
  ///
  /// In en, this message translates to:
  /// **'Your data'**
  String get sYourData;

  /// No description provided for @sfBeforeYouMeet.
  ///
  /// In en, this message translates to:
  /// **'Before you meet'**
  String get sfBeforeYouMeet;

  /// No description provided for @sfTip1.
  ///
  /// In en, this message translates to:
  /// **'Meet in a public place, and tell someone where you are going.'**
  String get sfTip1;

  /// No description provided for @sfTip2.
  ///
  /// In en, this message translates to:
  /// **'Arrange your own way there and back.'**
  String get sfTip2;

  /// No description provided for @sfTip3.
  ///
  /// In en, this message translates to:
  /// **'Never send money, gift cards or crypto to someone you have not met.'**
  String get sfTip3;

  /// No description provided for @sfTip4.
  ///
  /// In en, this message translates to:
  /// **'Keep chatting here until you trust them: links and other apps are where scams continue.'**
  String get sfTip4;

  /// No description provided for @sfTip5.
  ///
  /// In en, this message translates to:
  /// **'If something feels wrong, leave. You can block and report from the chat menu at any time.'**
  String get sfTip5;

  /// No description provided for @sfDatePlans.
  ///
  /// In en, this message translates to:
  /// **'Date plans'**
  String get sfDatePlans;

  /// No description provided for @sfDatePlansHint.
  ///
  /// In en, this message translates to:
  /// **'Tell someone you trust where you will be. If you do not check in, they are emailed.'**
  String get sfDatePlansHint;

  /// No description provided for @sfNoPlans.
  ///
  /// In en, this message translates to:
  /// **'No plans right now.'**
  String get sfNoPlans;

  /// No description provided for @sfPeopleYouTrust.
  ///
  /// In en, this message translates to:
  /// **'People you trust'**
  String get sfPeopleYouTrust;

  /// No description provided for @sfPeopleHint.
  ///
  /// In en, this message translates to:
  /// **'They have to agree first, and only ever get what you choose to send. Up to three.'**
  String get sfPeopleHint;

  /// No description provided for @sfMyReports.
  ///
  /// In en, this message translates to:
  /// **'My reports'**
  String get sfMyReports;

  /// No description provided for @sfMyReportsHint.
  ///
  /// In en, this message translates to:
  /// **'See what happened to reports you sent'**
  String get sfMyReportsHint;

  /// No description provided for @sfNoReportsTitle.
  ///
  /// In en, this message translates to:
  /// **'No reports yet'**
  String get sfNoReportsTitle;

  /// No description provided for @sfNoReportsBody.
  ///
  /// In en, this message translates to:
  /// **'If someone breaks the rules, report them from their profile or the chat menu. You will see the result here.'**
  String get sfNoReportsBody;

  /// No description provided for @sfOutcomePending.
  ///
  /// In en, this message translates to:
  /// **'Waiting for a moderator to look.'**
  String get sfOutcomePending;

  /// No description provided for @sfOutcomeAction.
  ///
  /// In en, this message translates to:
  /// **'Reviewed: we took action. Thank you for telling us.'**
  String get sfOutcomeAction;

  /// No description provided for @sfOutcomeNone.
  ///
  /// In en, this message translates to:
  /// **'Reviewed: we found nothing that breaks the rules this time.'**
  String get sfOutcomeNone;

  /// No description provided for @sModerationUnavailable.
  ///
  /// In en, this message translates to:
  /// **'We can\'t check this right now. Please try again in a moment.'**
  String get sModerationUnavailable;
}

class _AppLocalizationsDelegate
    extends LocalizationsDelegate<AppLocalizations> {
  const _AppLocalizationsDelegate();

  @override
  Future<AppLocalizations> load(Locale locale) {
    return SynchronousFuture<AppLocalizations>(lookupAppLocalizations(locale));
  }

  @override
  bool isSupported(Locale locale) =>
      <String>['en', 'es', 'hi'].contains(locale.languageCode);

  @override
  bool shouldReload(_AppLocalizationsDelegate old) => false;
}

AppLocalizations lookupAppLocalizations(Locale locale) {
  // Lookup logic when only language code is specified.
  switch (locale.languageCode) {
    case 'en':
      return AppLocalizationsEn();
    case 'es':
      return AppLocalizationsEs();
    case 'hi':
      return AppLocalizationsHi();
  }

  throw FlutterError(
    'AppLocalizations.delegate failed to load unsupported locale "$locale". This is likely '
    'an issue with the localizations generation tool. Please file an issue '
    'on GitHub with a reproducible sample app and the gen-l10n configuration '
    'that was used.',
  );
}
